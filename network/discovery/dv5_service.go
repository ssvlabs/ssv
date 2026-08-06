package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/discover/v5wire"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/network/records"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/utils/ttl"
)

const (
	defaultDiscoveryInterval = 100 * time.Millisecond
	publishENRInterval       = 500 * time.Millisecond
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
	Ping(*enode.Node) (*v5wire.Pong, error)
	LocalNode() *enode.LocalNode
	Close()
}

// DiscV5Service wraps discover.UDPv5 with additional functionality
// it implements go-libp2p/core/discovery.Discovery
// currently using ENR entry (subnets) to facilitate subnets discovery
// TODO: should be changed once discv5 supports topics (v5.2)
type DiscV5Service struct {
	logger *zap.Logger

	ctx    context.Context
	cancel context.CancelFunc

	dv5Listener Listener
	bootnodes   []*enode.Node

	conns      peers.ConnectionIndex
	subnetsIdx peers.SubnetsIndex

	// discoveredPeersPool keeps track of recently discovered peers so we can rank them and choose
	// the best candidates to connect to.
	discoveredPeersPool *ttl.Map[peer.ID, DiscoveredPeer]
	// trimmedRecently keeps track of recently trimmed peers so we don't try to connect to these
	// shortly after we've trimmed these (we still might consider connecting to these once they
	// are removed from this map after some time passes)
	trimmedRecently *ttl.Map[peer.ID, struct{}]

	conn       *net.UDPConn
	sharedConn *SharedUDPConn
	// socketConn wraps conn to time socket reads; the post-fork listener drains
	// through it, so DiscoveryStale can detect a wedged socket.
	socketConn *TimedConn

	ssvConfig *networkconfig.SSV
	subnets   commons.Subnets

	publishLock chan struct{}

	closeOnce sync.Once
	closeErr  error
}

func NewDiscV5Service(pctx context.Context, logger *zap.Logger, opts *Options) (*DiscV5Service, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(pctx)
	dvs := DiscV5Service{
		logger:              logger.Named(log.NameDiscoveryService),
		ctx:                 ctx,
		cancel:              cancel,
		conns:               opts.ConnIndex,
		subnetsIdx:          opts.SubnetsIdx,
		ssvConfig:           opts.SSVConfig,
		subnets:             opts.DiscV5Opts.Subnets,
		publishLock:         make(chan struct{}, 1),
		discoveredPeersPool: opts.DiscoveredPeersPool,
		trimmedRecently:     opts.TrimmedRecently,
	}

	logger.Debug(
		"configuring discv5 discovery",
		zap.Any("discV5Opts", opts.DiscV5Opts),
		zap.Any("hostAddress", opts.HostAddress),
		zap.Any("hostDNS", opts.HostDNS),
	)
	if err := dvs.initDiscV5Listener(opts); err != nil {
		// initDiscV5Listener unwinds its own resources, but the context we
		// derived above is ours to release on this failure path — the discarded
		// service's Close (which would cancel it) never runs.
		cancel()
		return nil, err
	}
	return &dvs, nil
}

// Close implements io.Closer and is idempotent: a repeat call would panic
// re-closing sharedConn.Unhandled, so the guard lives here. The sole production
// caller (p2pNetwork.Close) already closes once, but tests and any future caller
// reach this directly.
func (dvs *DiscV5Service) Close() error {
	dvs.closeOnce.Do(func() {
		dvs.closeErr = dvs.close()
	})
	return dvs.closeErr
}

func (dvs *DiscV5Service) close() error {
	if dvs.cancel != nil {
		dvs.cancel()
	}
	// Order matters. The post-fork listener produces into sharedConn.Unhandled,
	// so it must be fully stopped before that channel is closed — closing it
	// mid-send panics the producer. Stopping the listeners first is safe:
	// SharedUDPConn.Close releases the pre-fork reader, and draining continues
	// throughout so the post-fork listener cannot wedge on its way down.
	if dvs.dv5Listener != nil {
		dvs.dv5Listener.Close()
	}
	if dvs.sharedConn != nil {
		close(dvs.sharedConn.Unhandled)
		dvs.sharedConn.WaitDrained()
	}
	if dvs.conn != nil {
		// The post-fork listener owns this socket and closes it on shutdown, so
		// finding it already closed is expected rather than an error.
		if err := dvs.conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			return err
		}
	}
	return nil
}

// DiscoveryStale reports whether the discv5 socket has gone unread for longer
// than grace — the sign discovery has wedged. False before the listener is built
// (or after a failed init), when there's nothing to judge.
//
// The operator health check polls this periodically, so it doubles as the
// sampling point for the read-staleness gauge.
func (dvs *DiscV5Service) DiscoveryStale(grace time.Duration) bool {
	if dvs.socketConn == nil {
		return false
	}
	age, ok := dvs.socketConn.ReadStaleness()
	recordDiscoveryReadStaleness(dvs.ctx, int64(age.Seconds()))
	return ok && age > grace
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
	pk, err := commons.ECDSAPubFromInterface(pki)
	if err != nil {
		return nil, fmt.Errorf("convert peer public key: %w", err)
	}
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
//
// All peer filtering is done inside checkPeer (not as discover() filters) so that
// every skip reason is tracked with structured metrics and windowed summary logs.
func (dvs *DiscV5Service) Bootstrap(handler HandleNewPeer) error {
	const logWindowSize = 50

	var (
		skippedPeersTotalPerWindow   uint64
		skippedByReasonPerWindow     = map[skipReason]uint64{}
		skippedPeersUnknownPerWindow uint64
	)

	dvs.discover(
		dvs.ctx,
		func(e PeerEvent) {
			err := dvs.checkPeer(dvs.ctx, e)
			if err != nil {
				skippedPeersTotalPerWindow++
				var skipErr *peerSkipError
				if errors.As(err, &skipErr) {
					skippedByReasonPerWindow[skipErr.reason]++
				} else {
					skippedPeersUnknownPerWindow++
				}
				if skippedPeersTotalPerWindow >= logWindowSize {
					summaryFields := discoverySkipSummaryFields(skippedPeersTotalPerWindow, skippedByReasonPerWindow, skippedPeersUnknownPerWindow)
					summaryFields = append(summaryFields,
						zap.Int("discovered_peers_pool_size", dvs.discoveredPeersPool.SlowLen()),
						zap.Int("trimmed_recently_size", dvs.trimmedRecently.SlowLen()),
					)
					dvs.logger.Debug("discovery skipped peers summary", summaryFields...)
					skippedPeersTotalPerWindow = 0
					skippedByReasonPerWindow = map[skipReason]uint64{}
					skippedPeersUnknownPerWindow = 0
				}
				return
			}
			handler(e)
		},
		defaultDiscoveryInterval,
	)

	return nil
}

type peerSkipError struct {
	reason skipReason
	err    error
}

func (e *peerSkipError) Error() string {
	return e.err.Error()
}

func (e *peerSkipError) Unwrap() error {
	return e.err
}

func newPeerSkipError(reason skipReason, err error) error {
	return &peerSkipError{reason: reason, err: err}
}

func discoverySkipSummaryFields(skippedPeers uint64, skippedByReason map[skipReason]uint64, skippedUnknown uint64) []zap.Field {
	fieldsOut := []zap.Field{
		zap.Uint64("skipped_peers_total", skippedPeers),
	}
	if skippedUnknown > 0 {
		fieldsOut = append(fieldsOut, zap.Uint64("skipped_unknown_reason", skippedUnknown))
	}

	reasons := make([]string, 0, len(skippedByReason))
	for reason := range skippedByReason {
		reasons = append(reasons, string(reason))
	}
	sort.Strings(reasons)
	for _, reason := range reasons {
		fieldsOut = append(fieldsOut, zap.Uint64("skipped_"+reason, skippedByReason[skipReason(reason)]))
	}

	return fieldsOut
}

func (dvs *DiscV5Service) checkPeer(ctx context.Context, e PeerEvent) error {
	pid := e.AddrInfo.ID
	if pid == "" {
		var err error
		pid, err = PeerID(e.Node)
		if err != nil {
			recordPeerSkipped(ctx, skipReasonInvalidPeerID)
			return newPeerSkipError(skipReasonInvalidPeerID, fmt.Errorf("could not get peer ID from node record: %w", err))
		}
	}

	isSSV, err := readSSVNodeFlag(e.Node)
	if err != nil {
		recordPeerSkipped(ctx, skipReasonNotSSV)
		return newPeerSkipError(skipReasonNotSSV, fmt.Errorf("could not read ssv entry: %w", err))
	}
	if !isSSV {
		recordPeerSkipped(ctx, skipReasonNotSSV)
		return newPeerSkipError(skipReasonNotSSV, errors.New("node is not an SSV node"))
	}

	// Get the peer's domain type, skipping unless it matches our current or next domain.
	// We advertise the static current domain in our main ENR key, but fork-aware clients
	// (e.g. Anchor) advertise the active domain, which becomes the next domain after a fork —
	// so a strict match against the current domain would reject them forever. Real fork
	// enforcement happens in the fork-aware handshake filter and per-slot message validation,
	// not here.
	peerDiscoveriesCounter.Add(ctx, 1)
	nodeDomainType, err := records.GetDomainTypeEntry(e.Node.Record(), records.KeyDomainType)
	if err != nil {
		recordPeerSkipped(ctx, skipReasonInvalidDomainType)
		return newPeerSkipError(skipReasonInvalidDomainType, fmt.Errorf("could not read domain type: %w", err))
	}
	// Only accept NextDomainType when it's set to a distinct value: config parsing defaults
	// it to DomainType, and a zero value must not make us accept peers advertising an
	// all-zero domain.
	hasDistinctNextDomain := dvs.ssvConfig.NextDomainType != (spectypes.DomainType{}) &&
		dvs.ssvConfig.NextDomainType != dvs.ssvConfig.DomainType
	matchesDomain := nodeDomainType == dvs.ssvConfig.DomainType
	matchesNextDomain := hasDistinctNextDomain && nodeDomainType == dvs.ssvConfig.NextDomainType
	if !matchesDomain && !matchesNextDomain {
		recordPeerSkipped(ctx, skipReasonDomainTypeMismatch)
		var err error
		if hasDistinctNextDomain {
			err = fmt.Errorf("domain type %x matches neither %x nor %x", nodeDomainType, dvs.ssvConfig.DomainType, dvs.ssvConfig.NextDomainType)
		} else {
			err = fmt.Errorf("domain type %x does not match %x", nodeDomainType, dvs.ssvConfig.DomainType)
		}
		return newPeerSkipError(skipReasonDomainTypeMismatch, err)
	}

	// Get the peer's subnets, skipping if it has none.
	peerSubnets, err := records.GetSubnetsEntry(e.Node.Record())
	if err != nil {
		recordPeerSkipped(ctx, skipReasonInvalidSubnets)
		return newPeerSkipError(skipReasonInvalidSubnets, fmt.Errorf("could not read subnets: %w", err))
	}
	if commons.ZeroSubnets == peerSubnets {
		recordPeerSkipped(ctx, skipReasonZeroSubnets)
		return newPeerSkipError(skipReasonZeroSubnets, errors.New("zero subnets"))
	}

	dvs.subnetsIdx.UpdatePeerSubnets(pid, peerSubnets)

	if dvs.conns.IsBad(pid) {
		recordPeerSkipped(ctx, skipReasonBadPeer)
		return newPeerSkipError(skipReasonBadPeer, errors.New("peer is marked bad"))
	}

	if dvs.conns.Connectedness(pid) == network.Connected {
		recordPeerSkipped(ctx, skipReasonAlreadyConnected)
		return newPeerSkipError(skipReasonAlreadyConnected, errors.New("peer already connected"))
	}

	if dvs.trimmedRecently.Has(pid) {
		recordPeerSkipped(ctx, skipReasonRecentlyTrimmed)
		return newPeerSkipError(skipReasonRecentlyTrimmed, errors.New("peer was trimmed recently"))
	}

	sharedSubnets := dvs.subnets.SharedSubnets(peerSubnets)
	if len(sharedSubnets) == 0 {
		recordPeerSkipped(ctx, skipReasonNoSharedSubnets)
		return newPeerSkipError(skipReasonNoSharedSubnets, fmt.Errorf("no shared subnets: own=%s peer=%s", dvs.subnets.StringHumanReadable(), peerSubnets.StringHumanReadable()))
	}

	if dvs.discoveredPeersPool.Has(pid) {
		recordPeerSkipped(ctx, skipReasonAlreadyDiscovered)
		return newPeerSkipError(skipReasonAlreadyDiscovered, errors.New("peer already discovered recently"))
	}

	// Filters
	if !dvs.limitNodeFilter(e.Node) {
		recordPeerSkipped(ctx, skipReasonReachedLimit)
		return newPeerSkipError(skipReasonReachedLimit, errors.New("reached limit"))
	}

	peerAcceptedCounter.Add(ctx, 1)

	return nil
}

// listenV5 is indirected so tests can wrap or fail listener creation to
// exercise initDiscV5Listener's error-path cleanup; it returns the Listener
// interface, not *discover.UDPv5, so a wrapper can be substituted.
var listenV5 = func(conn discover.UDPConn, ln *enode.LocalNode, cfg discover.Config) (Listener, error) {
	// Return an untyped nil on error, never a (*discover.UDPv5)(nil) wrapped in
	// the interface, so a nil check on the result is meaningful for any caller.
	listener, err := discover.ListenV5(conn, ln, cfg)
	if err != nil {
		return nil, err
	}
	return listener, nil
}

// initDiscV5Listener creates a new listener and starts it
func (dvs *DiscV5Service) initDiscV5Listener(discOpts *Options) (err error) {
	opts := discOpts.DiscV5Opts
	if err := opts.Validate(); err != nil {
		return fmt.Errorf("invalid opts: %w", err)
	}

	ipAddr, bindIP, n := opts.IPs()

	udpConn, err := newUDPListener(bindIP, opts.Port, n)
	if err != nil {
		return fmt.Errorf("could not listen UDP: %w", err)
	}
	dvs.conn = udpConn

	// Wrap the socket for liveness (see DiscoveryStale): only the post-fork
	// listener drains it, so only it gets the wrapped conn — the pre-fork
	// listener reads sharedConn's buffer, not the socket.
	socketConn := NewTimedConn(udpConn)
	dvs.socketConn = socketConn

	// Registered before anything else can fail, so every error path below
	// releases the socket. Runs last of the deferred cleanups, by which point a
	// listener may already have closed it.
	defer func() {
		if err != nil {
			_ = udpConn.Close()
			dvs.conn = nil
			dvs.socketConn = nil
		}
	}()

	localNode, err := dvs.createLocalNode(discOpts, ipAddr)
	if err != nil {
		return fmt.Errorf("could not create local node: %w", err)
	}

	// Get the protocol ID, or set to default if not provided
	protocolID := dvs.ssvConfig.DiscoveryProtocolID
	emptyProtocolID := [6]byte{}
	if protocolID == emptyProtocolID {
		protocolID = DefaultSSVProtocolID
	}

	// New discovery, with ProtocolID restriction, to be kept post-fork
	unhandled := make(chan discover.ReadPacket, unhandledChanSize)
	sharedConn := NewSharedUDPConn(dvs.ctx, dvs.logger, udpConn, unhandled)
	dvs.sharedConn = sharedConn

	// Everything below can fail before dv5Listener is set, and the caller drops
	// the half-built service without calling Close, so unwind here instead. The
	// order mirrors Close: stop the producer first, since by the time the
	// pre-fork listener is being built the post-fork one is already forwarding
	// into unhandled, and closing that channel under a live producer panics.
	var postForkListener Listener
	defer func() {
		if err == nil {
			return
		}
		if postForkListener != nil {
			postForkListener.Close() // also closes udpConn
		}
		_ = sharedConn.Close() // never returns an error
		close(unhandled)
		sharedConn.WaitDrained()
		dvs.sharedConn = nil
	}()

	dv5PostForkCfg, err := opts.DiscV5Cfg(dvs.logger, WithProtocolID(protocolID), WithUnhandled(unhandled))
	if err != nil {
		return err
	}

	dv5PostForkListener, err := listenV5(socketConn, localNode, *dv5PostForkCfg)
	if err != nil {
		return fmt.Errorf("could not create discV5 listener: %w", err)
	}
	postForkListener = dv5PostForkListener

	dvs.logger.Debug("started discv5 post-fork listener (UDP)",
		fields.BindIP(bindIP),
		zap.Uint16("UdpPort", opts.Port),
		fields.ENRLocalNode(localNode),
		fields.Domain(discOpts.SSVConfig.DomainType),
		fields.ProtocolID(protocolID),
	)

	// Previous discovery, without ProtocolID restriction, to be discontinued after the fork
	dv5PreForkCfg, err := opts.DiscV5Cfg(dvs.logger)
	if err != nil {
		return err
	}

	dv5PreForkListener, err := listenV5(sharedConn, localNode, *dv5PreForkCfg)
	if err != nil {
		return fmt.Errorf("could not create discV5 pre-fork listener: %w", err)
	}

	dvs.logger.Debug("started discv5 pre-fork listener (UDP)",
		fields.BindIP(bindIP),
		zap.Uint16("UdpPort", opts.Port),
		fields.ENRLocalNode(localNode),
		fields.Domain(discOpts.SSVConfig.DomainType),
	)

	dvs.dv5Listener = NewForkingDV5Listener(dvs.logger, dv5PreForkListener, dv5PostForkListener, 5*time.Second)
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
func (dvs *DiscV5Service) RegisterSubnets(subnets ...uint64) (updated bool, err error) {
	if len(subnets) == 0 {
		return false, nil
	}
	updatedSubnets, isUpdated, err := records.UpdateSubnets(dvs.dv5Listener.LocalNode(), subnets, nil)
	if err != nil {
		return false, fmt.Errorf("could not update ENR: %w", err)
	}
	if isUpdated {
		dvs.subnets = updatedSubnets
		dvs.logger.Debug("updated subnets", fields.UpdatedENRLocalNode(dvs.dv5Listener.LocalNode()))
		return true, nil
	}
	return false, nil
}

// DeregisterSubnets removes the given subnets and publish the updated node record
func (dvs *DiscV5Service) DeregisterSubnets(subnets ...uint64) (updated bool, err error) {
	if len(subnets) == 0 {
		return false, nil
	}
	updatedSubnets, isUpdated, err := records.UpdateSubnets(dvs.dv5Listener.LocalNode(), nil, subnets)
	if err != nil {
		return false, fmt.Errorf("could not update ENR: %w", err)
	}
	if isUpdated {
		dvs.subnets = updatedSubnets
		dvs.logger.Debug("updated subnets", fields.UpdatedENRLocalNode(dvs.dv5Listener.LocalNode()))
		return true, nil
	}
	return false, nil
}

// PublishENR publishes the ENR with the current domain type across the network
func (dvs *DiscV5Service) PublishENR() {
	// Update own node record.
	err := records.SetDomainTypeEntry(dvs.dv5Listener.LocalNode(), records.KeyDomainType, dvs.ssvConfig.DomainType)
	if err != nil {
		dvs.logger.Error("could not set domain type", zap.Error(err))
		return
	}
	// KeyNextDomainType carries the real upcoming domain (not the current one) so that
	// fork-aware (boole-style) binaries and future dynamic domain flips can rely on it;
	// our own discovery filter accepts peers advertising either the current or next domain.
	err = records.SetDomainTypeEntry(dvs.dv5Listener.LocalNode(), records.KeyNextDomainType, dvs.ssvConfig.NextDomainType)
	if err != nil {
		dvs.logger.Error("could not set next domain type", zap.Error(err))
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

	// Publish ENR by pinging random SSV nodes so they learn our updated record.
	// Minimal filtering: we only require valid SSV nodes that aren't marked bad.
	// We intentionally omit connection-state filters (alreadyConnected, recentlyTrimmed)
	// because wider propagation is more important — pings are cheap and already-connected
	// peers should also learn about our ENR update. Subnet filters are also omitted
	// because ENR publication is a global broadcast, not subnet-specific.
	dvs.discover(ctx, func(e PeerEvent) {
		_, err := dvs.dv5Listener.Ping(e.Node)
		if err != nil {
			errs++
			if err.Error() == "RPC timeout" {
				// ignore
				return
			}
			dvs.logger.Warn("could not ping node", fields.TargetNodeENR(e.Node), zap.Error(err))
			return
		}
		pings++
		peerIDs[e.AddrInfo.ID] = struct{}{}
	}, publishENRInterval, dvs.ssvNodeFilter(), dvs.badNodeFilter())

	// Log metrics.
	dvs.logger.Debug("done publishing ENR",
		fields.Took(time.Since(start)),
		zap.Int("unique_peers", len(peerIDs)),
		zap.Int("pings", pings),
		zap.Int("errors", errs),
	)
}

func (dvs *DiscV5Service) createLocalNode(discOpts *Options, ipAddr net.IP) (*enode.LocalNode, error) {
	opts := discOpts.DiscV5Opts
	localNode, err := createLocalNode(opts.NetworkKey, opts.StoragePath, ipAddr, opts.Port, opts.TCPPort)
	if err != nil {
		return nil, fmt.Errorf("could not create local node: %w", err)
	}
	err = addAddresses(localNode, discOpts.HostAddress, discOpts.HostDNS)
	if err != nil {
		return nil, fmt.Errorf("could not add configured addresses: %w", err)
	}
	err = DecorateNode(
		localNode,

		// Satisfy decorations of forks supported by this node.
		// KeyNextDomainType carries the real upcoming domain, not the current one (see PublishENR).
		DecorateWithDomainType(records.KeyDomainType, dvs.ssvConfig.DomainType),
		DecorateWithDomainType(records.KeyNextDomainType, dvs.ssvConfig.NextDomainType),
		DecorateWithSubnets(opts.Subnets),
	)
	if err != nil {
		return nil, fmt.Errorf("could not decorate local node: %w", err)
	}

	logFields := []zapcore.Field{
		fields.ENRLocalNode(localNode),
		fields.Domain(dvs.ssvConfig.DomainType),
	}

	if opts.Subnets.HasActive() {
		logFields = append(logFields, zap.String("subnets", opts.Subnets.StringHumanReadable()))
	}

	dvs.logger.Debug("node record is ready", logFields...)

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
		return nil, fmt.Errorf("could not listen to UDP: %w", err)
	}
	return conn, nil
}
