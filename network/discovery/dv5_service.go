package discovery

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/network/records"
)

var (
	defaultDiscoveryInterval = time.Second
	publishENRTimeout        = time.Minute

	publishStateReady   = int32(0)
	publishStatePending = int32(1)
)

// NodeProvider is an interface for managing ENRs
type NodeProvider interface {
	Self() *enode.LocalNode
	Node(logger *zap.Logger, info peer.AddrInfo) (*enode.Node, error)
}

// NodeFilter can be used for nodes filtering during discovery
type NodeFilter func(*enode.Node) bool

// DiscV5Service wraps discover.UDPv5 with additional functionality
// it implements go-libp2p/core/discovery.Discovery
// currently using ENR entry (subnets) to facilitate subnets discovery
// TODO: should be changed once discv5 supports topics (v5.2)
type DiscV5Service struct {
	ctx    context.Context
	cancel context.CancelFunc

	dv5Listener *discover.UDPv5
	bootnodes   []*enode.Node

	conns      peers.ConnectionIndex
	subnetsIdx peers.SubnetsIndex

	publishState int32
	conn         *net.UDPConn

	domainType spectypes.DomainType
	subnets    []byte
}

func newDiscV5Service(pctx context.Context, logger *zap.Logger, discOpts *Options) (Service, error) {
	ctx, cancel := context.WithCancel(pctx)
	dvs := DiscV5Service{
		ctx:          ctx,
		cancel:       cancel,
		publishState: publishStateReady,
		conns:        discOpts.ConnIndex,
		subnetsIdx:   discOpts.SubnetsIdx,
		domainType:   discOpts.DomainType,
		subnets:      discOpts.DiscV5Opts.Subnets,
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

	dvs.discover(dvs.ctx, func(e PeerEvent) {
		logger := logger.With(
			fields.ENR(e.Node),
			fields.PeerID(e.AddrInfo.ID),
		)
		err := dvs.checkPeer(logger, e)
		if err != nil {
			logger.Debug("discovered peer was dropped", zap.Error(err))
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
	nodeDomainType, err := records.GetDomainTypeEntry(e.Node.Record())
	if err != nil {
		// TODO: skip missing domain type (likely old node).
	} else if nodeDomainType != dvs.domainType {
		// TODO: skip different domain type.
	}

	// Get the peer's subnets, skipping if it has none.
	nodeSubnets, err := records.GetSubnetsEntry(e.Node.Record())
	if err != nil {
		return fmt.Errorf("could not read subnets: %w", err)
	}
	if bytes.Equal(zeroSubnets, nodeSubnets) {
		return errors.New("zero subnets")
	}

	dvs.subnetsIdx.UpdatePeerSubnets(e.AddrInfo.ID, nodeSubnets)
	if !dvs.limitNodeFilter(e.Node) && !dvs.sharedSubnetsFilter(1)(e.Node) {
		metricRejectedNodes.Inc()
		return errors.New("no shared subnets")
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

	dv5Cfg, err := opts.DiscV5Cfg(logger)
	if err != nil {
		return err
	}
	dv5Listener, err := discover.ListenV5(udpConn, localNode, *dv5Cfg)
	if err != nil {
		return errors.Wrap(err, "could not create discV5 listener")
	}
	dvs.dv5Listener = dv5Listener
	dvs.bootnodes = dv5Cfg.Bootnodes

	logger.Debug("started discv5 listener (UDP)", fields.BindIP(bindIP),
		zap.Int("UdpPort", opts.Port), fields.ENRLocalNode(localNode), fields.Domain(discOpts.DomainType))

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

	t := time.NewTimer(interval)
	defer t.Stop()
	wait := func() {
		t.Reset(interval)
		<-t.C
	}

	for ctx.Err() == nil {
		wait()
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
func (dvs *DiscV5Service) RegisterSubnets(logger *zap.Logger, subnets ...int) error {
	if len(subnets) == 0 {
		return nil
	}
	updated, err := records.UpdateSubnets(dvs.dv5Listener.LocalNode(), commons.Subnets(), subnets, nil)
	if err != nil {
		return errors.Wrap(err, "could not update ENR")
	}
	if updated != nil {
		dvs.subnets = updated
		logger.Debug("updated subnets", fields.UpdatedENRLocalNode(dvs.dv5Listener.LocalNode()))
		go dvs.publishENR(logger)
	}
	return nil
}

// DeregisterSubnets removes the given subnets and publish the updated node record
func (dvs *DiscV5Service) DeregisterSubnets(logger *zap.Logger, subnets ...int) error {
	logger = logger.Named(logging.NameDiscoveryService)

	if len(subnets) == 0 {
		return nil
	}
	updated, err := records.UpdateSubnets(dvs.dv5Listener.LocalNode(), commons.Subnets(), nil, subnets)
	if err != nil {
		return errors.Wrap(err, "could not update ENR")
	}
	if updated != nil {
		dvs.subnets = updated
		logger.Debug("updated subnets", fields.UpdatedENRLocalNode(dvs.dv5Listener.LocalNode()))
		go dvs.publishENR(logger)
	}
	return nil
}

// publishENR publishes the new ENR across the network
func (dvs *DiscV5Service) publishENR(logger *zap.Logger) {
	ctx, done := context.WithTimeout(dvs.ctx, publishENRTimeout)
	defer done()
	if !atomic.CompareAndSwapInt32(&dvs.publishState, publishStateReady, publishStatePending) {
		// pending
		logger.Debug("pending publish ENR")
		return
	}
	defer atomic.StoreInt32(&dvs.publishState, publishStateReady)
	dvs.discover(ctx, func(e PeerEvent) {
		metricPublishEnrPings.Inc()
		err := dvs.dv5Listener.Ping(e.Node)
		if err != nil {
			if err.Error() == "RPC timeout" {
				// ignore
				return
			}
			logger.Warn("could not ping node", fields.TargetNodeENR(e.Node), zap.Error(err))
			return
		}
		metricPublishEnrPongs.Inc()
		// logger.Debug("ping success", logging.TargetNodeEnr(e.Node))
	}, time.Millisecond*100, dvs.badNodeFilter(logger))
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
		DecorateWithDomainType(dvs.domainType),
		DecorateWithSubnets(opts.Subnets),
	)
	if err != nil {
		return nil, errors.Wrap(err, "could not decorate local node")
	}

	logger.Debug("node record is ready", fields.ENRLocalNode(localNode), fields.Domain(dvs.domainType), fields.Subnets(opts.Subnets))

	return localNode, nil
}

// newUDPListener creates a udp server
func newUDPListener(bindIP net.IP, port int, network string) (*net.UDPConn, error) {
	udpAddr := &net.UDPAddr{
		IP:   bindIP,
		Port: port,
	}
	conn, err := net.ListenUDP(network, udpAddr)
	if err != nil {
		return nil, errors.Wrap(err, "could not listen to UDP")
	}
	return conn, nil
}
