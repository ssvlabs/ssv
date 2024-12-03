package discovery

import (
	"bytes"
	"context"
	"fmt"
	"github.com/ssvlabs/ssv/network/topics"
	"github.com/ssvlabs/ssv/utils/hashmap"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/network/records"
	"github.com/ssvlabs/ssv/networkconfig"
)

// counters to keep track of discovered/connected peers (per subnet) through discovery mechanism ONLY,
// this means there might be other peers (we've connected to before/outside-of the means of discovery
// mechanism) who contribute to some subnets our SSV node is interested in - we just don't track these
// contributions with these counters below.
// Note also, these counters hold stats across all 128 subnets (not just the ones we are interested in).
// For example, ConnectedSubnetsCounter will contain/maintain a list of peers even for subnets
// that are irrelevant for our own SSV node (they are just there to reflect what subnets
// each peer we connected to offers).
var (
	// CountersMtx helps with updating counters related to peer discovery/connecting in
	// atomic fashion (like updating ConnectedSubnetsCounter while iterating DiscoveredSubnetsCounter
	// map) to happen atomically.
	// TODO - we don't need hashmap then, use plain simple golang map.
	CountersMtx sync.Mutex
	// DiscoveredSubnetsCounter contains subnet->peerIDs mapping for peers discovery service found.
	DiscoveredSubnetsCounter = hashmap.New[int, []peer.ID]()
	// ConnectedSubnetsCounter contains subnet->peerIDs mapping for peers previously added to
	// DiscoveredSubnetsCounter map. This means ConnectedSubnetsCounter doesn't actually reflect/count
	// all the active peer connections we have, but rather only those we've made with the help of
	// discovery service (which is more like a half of all connections SSV node typically makes).
	ConnectedSubnetsCounter = hashmap.New[int, []peer.ID]()
	// Discovered1stTimeSubnetsCounter is subnet set that represents those subnets we've discovered a
	// peer for (we add subnet to this set the very first time it happened).
	Discovered1stTimeSubnetsCounter = hashmap.New[int, int64]()
	// Connected1stTimeSubnetsCounter is subnet set that represents those subnets we've connected a
	// peer for (we add subnet to this set the very first time it happened).
	Connected1stTimeSubnetsCounter = hashmap.New[int, int64]()
)

var (
	defaultDiscoveryInterval = time.Millisecond * 1
	publishENRTimeout        = time.Minute
)

// NodeProvider is an interface for managing ENRs
type NodeProvider interface {
	Self() *enode.LocalNode
	Node(logger *zap.Logger, info peer.AddrInfo) (*enode.Node, error)
}

// NodeFilter can be used for nodes filtering during discovery
type NodeFilter func(*enode.Node) bool

type Listener interface {
	Lookup(enode.ID) []*enode.Node
	RandomNodes() enode.Iterator
	AllNodes() []*enode.Node
	Ping(*enode.Node) error
	LocalNode() *enode.LocalNode
	Close()
}

// DiscV5Service wraps discover.UDPv5 with additional functionality
// it implements go-libp2p/core/discovery.Discovery
// currently using ENR entry (subnets) to facilitate subnets discovery
// TODO: should be changed once discv5 supports topics (v5.2)
type DiscV5Service struct {
	ctx    context.Context
	cancel context.CancelFunc

	dv5Listener Listener
	bootnodes   []*enode.Node
	topicsCtrl  topics.Controller

	conns      peers.ConnectionIndex
	subnetsIdx peers.SubnetsIndex

	conn       *net.UDPConn
	sharedConn *SharedUDPConn

	networkConfig networkconfig.NetworkConfig
	subnets       []byte

	publishLock chan struct{}
}

func newDiscV5Service(
	pctx context.Context,
	logger *zap.Logger,
	topicsController topics.Controller,
	discOpts *Options,
) (Service, error) {
	ctx, cancel := context.WithCancel(pctx)
	dvs := DiscV5Service{
		ctx:           ctx,
		cancel:        cancel,
		topicsCtrl:    topicsController,
		conns:         discOpts.ConnIndex,
		subnetsIdx:    discOpts.SubnetsIdx,
		networkConfig: discOpts.NetworkConfig,
		subnets:       discOpts.DiscV5Opts.Subnets,
		publishLock:   make(chan struct{}, 1),
	}

	logger.Debug("configuring discv5 discovery", zap.Any("discOpts", discOpts))
	if err := dvs.initDiscV5Listener(logger, discOpts); err != nil {
		return nil, err
	}
	return &dvs, nil
}

// Close implements io.Closer
func (dvs *DiscV5Service) Close() error {
	if dvs.cancel != nil {
		dvs.cancel()
	}
	if dvs.conn != nil {
		if err := dvs.conn.Close(); err != nil {
			return err
		}
	}
	if dvs.sharedConn != nil {
		close(dvs.sharedConn.Unhandled)
	}
	if dvs.dv5Listener != nil {
		dvs.dv5Listener.Close()
	}
	return nil
}

// Self returns self node
func (dvs *DiscV5Service) Self() *enode.LocalNode {
	return dvs.dv5Listener.LocalNode()
}

// Node tries to find the enode.Node of the given peer
func (dvs *DiscV5Service) Node(logger *zap.Logger, info peer.AddrInfo) (*enode.Node, error) {
	pki, err := info.ID.ExtractPublicKey()
	if err != nil {
		return nil, err
	}
	pk := commons.ECDSAPubFromInterface(pki)
	id := enode.PubkeyToIDV4(pk)
	logger = logger.With(zap.String("info", info.String()),
		zap.String("enode.ID", id.String()))
	nodes := dvs.dv5Listener.AllNodes()
	node := findNode(nodes, id)
	if node == nil {
		logger.Debug("could not find node, trying lookup")
		// could not find node, trying to look it up
		nodes = dvs.dv5Listener.Lookup(id)
		node = findNode(nodes, id)
	}
	return node, nil
}

// Bootstrap start looking for new nodes, note that this function blocks.
// if we reached peers limit, make sure to accept peers with more than 1 shared subnet,
// which lets other components to determine whether we'll want to connect to this node or not.
func (dvs *DiscV5Service) Bootstrap(logger *zap.Logger, handler HandleNewPeer) error {
	logger = logger.Named(logging.NameDiscoveryService)

	// Log every 10th skipped peer.
	// TODO: remove once we've merged https://github.com/ssvlabs/ssv/pull/1803
	const logFrequency = 10
	var skippedPeers uint64 = 0

	dvs.discover(dvs.ctx, func(e PeerEvent) {
		logger := logger.With(
			fields.ENR(e.Node),
			fields.PeerID(e.AddrInfo.ID),
		)
		err := dvs.checkPeer(logger, e)
		if err != nil {
			if skippedPeers%logFrequency == 0 {
				logger.Debug("skipped discovered peer", zap.Error(err))
			}
			skippedPeers++
			return
		}
		handler(e)
	}, defaultDiscoveryInterval) // , dvs.forkVersionFilter) //, dvs.badNodeFilter)

	return nil
}

var zeroSubnets, _ = records.Subnets{}.FromString(records.ZeroSubnets)

func (dvs *DiscV5Service) checkPeer(logger *zap.Logger, e PeerEvent) error {
	// Get the peer's domain type, skipping if it mismatches ours.
	// TODO: uncomment errors once there are sufficient nodes with domain type.
	nodeDomainType, err := records.GetDomainTypeEntry(e.Node.Record(), records.KeyDomainType)
	if err != nil {
		return errors.Wrap(err, "could not read domain type")
	}
	nodeNextDomainType, err := records.GetDomainTypeEntry(e.Node.Record(), records.KeyNextDomainType)
	if err != nil && !errors.Is(err, records.ErrEntryNotFound) {
		return errors.Wrap(err, "could not read domain type")
	}
	if dvs.networkConfig.DomainType() != nodeDomainType &&
		dvs.networkConfig.DomainType() != nodeNextDomainType {
		return fmt.Errorf("mismatched domain type: neither %x nor %x match %x",
			nodeDomainType, nodeNextDomainType, dvs.networkConfig.DomainType())
	}

	// Get the peer's subnets, skipping if it has none.
	peerSubnets, err := records.GetSubnetsEntry(e.Node.Record())
	if err != nil {
		return fmt.Errorf("could not read subnets: %w", err)
	}
	if bytes.Equal(zeroSubnets, peerSubnets) {
		return errors.New("zero subnets")
	}

	dvs.subnetsIdx.UpdatePeerSubnets(e.AddrInfo.ID, peerSubnets)

	CountersMtx.Lock()
	for subnet, subnetActive := range peerSubnets {
		if subnetActive != 1 {
			continue // not interested in subnet that's not active (or no longer active)
		}

		_, ok := Discovered1stTimeSubnetsCounter.Get(subnet)
		if !ok {
			logger.Debug(
				"discovered subnet 1st time!",
				zap.Int("subnet_id", subnet),
				zap.String("peer_id", string(e.AddrInfo.ID)),
			)
			Discovered1stTimeSubnetsCounter.Set(subnet, 1)
		}

		peerIDs, _ := DiscoveredSubnetsCounter.Get(subnet)
		peerAlreadyDiscoveredForSubnet := false
		for _, peerID := range peerIDs {
			if peerID == e.AddrInfo.ID {
				// TODO - it's fine to get this warning occasionally, I guess ? Not sure what
				// it would mean though ... a peer who has advertised he has a subnet, but then
				// we discovered that he doesn't, and then peer says (still/again) that does
				// work with that subnet ...
				logger.Debug(
					"already discovered this subnet through this peer",
					zap.Int("subnet_id", subnet),
					zap.String("peer_id", string(peerID)),
				)
				peerAlreadyDiscoveredForSubnet = true
				break
			}
		}
		if !peerAlreadyDiscoveredForSubnet {
			DiscoveredSubnetsCounter.Set(subnet, append(peerIDs, e.AddrInfo.ID))
		}
	}
	CountersMtx.Unlock()

	// Filters
	if !dvs.limitNodeFilter(e.Node) {
		metricRejectedNodes.Inc()
		return errors.New("reached limit")
	}
	if !dvs.sharedSubnetsFilter(1)(e.Node) {
		metricRejectedNodes.Inc()
		return errors.New("no shared subnets")
	}

	// TODO - need a better (smart) way to do it, but for now try to connect only
	// peers that help with getting rid of dead subnets
	helpfulPeer := false
	subscribedTopics := dvs.topicsCtrl.Topics()
	for _, topic := range subscribedTopics {
		topicPeers, err := dvs.topicsCtrl.Peers(topic)
		if err != nil {
			return errors.Wrap(err, "could not get subscribed topic peers")
		}

		if len(topicPeers) >= 1 {
			continue // this topic has enough peers - TODO (1 is not enough tho)
		}

		// we've got a dead subnet here, see if this peer can help with that
		subnet, err := strconv.Atoi(topic)
		if err != nil {
			return errors.Wrap(err, "could not convert topic name to subnet id")
		}
		peerSubnet := peerSubnets[subnet]
		if peerSubnet != 1 {
			continue // peer doesn't have this subnet either, lets check other dead subnets we have
		}
		helpfulPeer = true // this peer helps with at least 1 dead subnet for us
		break
	}
	if !helpfulPeer {
		return errors.New("this peer doesn't help with dead subnets")
	}

	metricFoundNodes.Inc()
	return nil
}

// initDiscV5Listener creates a new listener and starts it
func (dvs *DiscV5Service) initDiscV5Listener(logger *zap.Logger, discOpts *Options) error {
	opts := discOpts.DiscV5Opts
	if err := opts.Validate(); err != nil {
		return errors.Wrap(err, "invalid opts")
	}

	ipAddr, bindIP, n := opts.IPs()

	udpConn, err := newUDPListener(bindIP, opts.Port, n)
	if err != nil {
		return errors.Wrap(err, "could not listen UDP")
	}
	dvs.conn = udpConn

	localNode, err := dvs.createLocalNode(logger, discOpts, ipAddr)
	if err != nil {
		return errors.Wrap(err, "could not create local node")
	}

	// Get the protocol ID, or set to default if not provided
	protocolID := dvs.networkConfig.DiscoveryProtocolID
	emptyProtocolID := [6]byte{}
	if protocolID == emptyProtocolID {
		protocolID = DefaultSSVProtocolID
	}

	// New discovery, with ProtocolID restriction, to be kept post-fork
	unhandled := make(chan discover.ReadPacket, 100) // size taken from https://github.com/ethereum/go-ethereum/blob/v1.13.5/p2p/server.go#L551
	sharedConn := &SharedUDPConn{udpConn, unhandled}
	dvs.sharedConn = sharedConn

	dv5PostForkCfg, err := opts.DiscV5Cfg(logger, WithProtocolID(protocolID), WithUnhandled(unhandled))
	if err != nil {
		return err
	}

	dv5PostForkListener, err := discover.ListenV5(udpConn, localNode, *dv5PostForkCfg)
	if err != nil {
		return errors.Wrap(err, "could not create discV5 listener")
	}

	logger.Debug("started discv5 post-fork listener (UDP)",
		fields.BindIP(bindIP),
		zap.Uint16("UdpPort", opts.Port),
		fields.ENRLocalNode(localNode),
		fields.Domain(discOpts.NetworkConfig.NextDomainType()),
		fields.ProtocolID(protocolID),
	)

	// Previous discovery, without ProtocolID restriction, to be discontinued after the fork
	dv5PreForkCfg, err := opts.DiscV5Cfg(logger)
	if err != nil {
		return err
	}

	dv5PreForkListener, err := discover.ListenV5(sharedConn, localNode, *dv5PreForkCfg)
	if err != nil {
		return errors.Wrap(err, "could not create discV5 pre-fork listener")
	}

	logger.Debug("started discv5 pre-fork listener (UDP)",
		fields.BindIP(bindIP),
		zap.Uint16("UdpPort", opts.Port),
		fields.ENRLocalNode(localNode),
		fields.Domain(discOpts.NetworkConfig.DomainType()),
	)

	dvs.dv5Listener = NewForkingDV5Listener(logger, dv5PreForkListener, dv5PostForkListener, 5*time.Second, dvs.networkConfig)
	dvs.bootnodes = dv5PreForkCfg.Bootnodes // Just take bootnodes from one of the config since they're equal

	return nil
}

// discover finds new nodes in the network,
// by a random walking on the underlying DHT.
//
// handler will act upon new node.
// interval enables to control the rate of new nodes that we find.
// filters will be applied on each new node before the handler is called,
// enabling to apply custom access control for different scenarios.
func (dvs *DiscV5Service) discover(ctx context.Context, handler HandleNewPeer, interval time.Duration, filters ...NodeFilter) {
	iterator := dvs.dv5Listener.RandomNodes()
	for _, f := range filters {
		iterator = enode.Filter(iterator, f)
	}
	// selfID is used to exclude current node
	selfID := dvs.dv5Listener.LocalNode().Node().ID().TerminalString()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for ctx.Err() == nil {
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
		exists := iterator.Next()
		if !exists {
			continue
		}
		// ignoring nil or self nodes
		if n := iterator.Node(); n != nil {
			if n.ID().TerminalString() == selfID {
				continue
			}
			ai, err := ToPeer(n)
			if err != nil {
				continue
			}
			handler(PeerEvent{
				AddrInfo: *ai,
				Node:     n,
			})
		}
	}
}

// RegisterSubnets adds the given subnets and publish the updated node record
func (dvs *DiscV5Service) RegisterSubnets(logger *zap.Logger, subnets ...uint64) (updated bool, err error) {
	if len(subnets) == 0 {
		return false, nil
	}
	updatedSubnets, err := records.UpdateSubnets(dvs.dv5Listener.LocalNode(), commons.Subnets(), subnets, nil)
	if err != nil {
		return false, errors.Wrap(err, "could not update ENR")
	}
	if updatedSubnets != nil {
		dvs.subnets = updatedSubnets
		logger.Debug("updated subnets", fields.UpdatedENRLocalNode(dvs.dv5Listener.LocalNode()))
		return true, nil
	}
	return false, nil
}

// DeregisterSubnets removes the given subnets and publish the updated node record
func (dvs *DiscV5Service) DeregisterSubnets(logger *zap.Logger, subnets ...uint64) (updated bool, err error) {
	logger = logger.Named(logging.NameDiscoveryService)

	if len(subnets) == 0 {
		return false, nil
	}
	updatedSubnets, err := records.UpdateSubnets(dvs.dv5Listener.LocalNode(), commons.Subnets(), nil, subnets)
	if err != nil {
		return false, errors.Wrap(err, "could not update ENR")
	}
	if updatedSubnets != nil {
		dvs.subnets = updatedSubnets
		logger.Debug("updated subnets", fields.UpdatedENRLocalNode(dvs.dv5Listener.LocalNode()))
		return true, nil
	}
	return false, nil
}

// PublishENR publishes the ENR with the current domain type across the network
func (dvs *DiscV5Service) PublishENR(logger *zap.Logger) {
	// Update own node record.
	err := records.SetDomainTypeEntry(dvs.dv5Listener.LocalNode(), records.KeyDomainType, dvs.networkConfig.DomainType())
	if err != nil {
		logger.Error("could not set domain type", zap.Error(err))
		return
	}
	err = records.SetDomainTypeEntry(dvs.dv5Listener.LocalNode(), records.KeyNextDomainType, dvs.networkConfig.NextDomainType())
	if err != nil {
		logger.Error("could not set next domain type", zap.Error(err))
		return
	}

	// Acquire publish lock to prevent parallel publishing.
	// If there's an ongoing goroutine, it would now start publishing the record updated above,
	// and if it's done before the new deadline, this goroutine would pick up where it left off.
	ctx, done := context.WithTimeout(dvs.ctx, publishENRTimeout)
	defer done()

	select {
	case <-ctx.Done():
		return
	case dvs.publishLock <- struct{}{}:
	}
	defer func() {
		// Release lock.
		<-dvs.publishLock
	}()

	// Collect some metrics.
	start := time.Now()
	pings, errs := 0, 0
	peerIDs := map[peer.ID]struct{}{}

	// Publish ENR.
	dvs.discover(ctx, func(e PeerEvent) {
		metricPublishEnrPings.Inc()
		err := dvs.dv5Listener.Ping(e.Node)
		if err != nil {
			errs++
			if err.Error() == "RPC timeout" {
				// ignore
				return
			}
			logger.Warn("could not ping node", fields.TargetNodeENR(e.Node), zap.Error(err))
			return
		}
		metricPublishEnrPongs.Inc()
		pings++
		peerIDs[e.AddrInfo.ID] = struct{}{}
	}, time.Millisecond*100, dvs.ssvNodeFilter(logger), dvs.badNodeFilter(logger))

	// Log metrics.
	logger.Debug("done publishing ENR",
		fields.Duration(start),
		zap.Int("unique_peers", len(peerIDs)),
		zap.Int("pings", pings),
		zap.Int("errors", errs))
}

func (dvs *DiscV5Service) createLocalNode(logger *zap.Logger, discOpts *Options, ipAddr net.IP) (*enode.LocalNode, error) {
	opts := discOpts.DiscV5Opts
	localNode, err := createLocalNode(opts.NetworkKey, opts.StoragePath, ipAddr, opts.Port, opts.TCPPort)
	if err != nil {
		return nil, errors.Wrap(err, "could not create local node")
	}
	err = addAddresses(localNode, discOpts.HostAddress, discOpts.HostDNS)
	if err != nil {
		return nil, errors.Wrap(err, "could not add configured addresses")
	}
	err = DecorateNode(
		localNode,

		// Satisfy decorations of forks supported by this node.
		DecorateWithDomainType(records.KeyDomainType, dvs.networkConfig.DomainType()),
		DecorateWithDomainType(records.KeyNextDomainType, dvs.networkConfig.NextDomainType()),
		DecorateWithSubnets(opts.Subnets),
	)
	if err != nil {
		return nil, errors.Wrap(err, "could not decorate local node")
	}

	logger.Debug("node record is ready", fields.ENRLocalNode(localNode), fields.Domain(dvs.networkConfig.DomainType()), fields.Subnets(opts.Subnets))

	return localNode, nil
}

// newUDPListener creates a udp server
func newUDPListener(bindIP net.IP, port uint16, network string) (*net.UDPConn, error) {
	udpAddr := &net.UDPAddr{
		IP:   bindIP,
		Port: int(port),
	}
	conn, err := net.ListenUDP(network, udpAddr)
	if err != nil {
		return nil, errors.Wrap(err, "could not listen to UDP")
	}
	return conn, nil
}
