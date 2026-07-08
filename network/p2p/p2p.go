package p2pv1

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	connmgrcore "github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/host"
	p2pnet "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/discovery"
	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/network/peers/connections"
	"github.com/ssvlabs/ssv/network/records"
	"github.com/ssvlabs/ssv/network/streams"
	"github.com/ssvlabs/ssv/network/topics"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/observability/log/fields"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/utils/async"
	"github.com/ssvlabs/ssv/utils/hashmap"
	"github.com/ssvlabs/ssv/utils/tasks"
	"github.com/ssvlabs/ssv/utils/ttl"
)

// network states
const (
	stateInitializing int32 = 0
	stateClosing      int32 = 1
	stateClosed       int32 = 2
	stateReady        int32 = 10
)

const (
	// peersTrimmingInterval defines how often we want to try and trim connected peers. This value
	// should be low enough for our node to find good set of peers reasonably fast (10-20 minutes)
	// after node start, but it shouldn't be too low since that might negatively affect Ethereum
	// duty execution quality.
	peersTrimmingInterval           = 30 * time.Second
	peersReportingInterval          = 60 * time.Second
	peerIdentitiesReportingInterval = 5 * time.Minute
	topicsReportingInterval         = 60 * time.Second
)

// PeersIndexProvider holds peers index instance
type PeersIndexProvider interface {
	PeersIndex() peers.Index
}

// HostProvider holds host instance
type HostProvider interface {
	Host() host.Host
}

// HealthChecker provides health checking capability for the node prober.
type HealthChecker interface {
	Healthy(ctx context.Context) error
}

// statusWithSubnet tracks a committee's subscription status along with the subnets it maps to
// under each fork, so we can subscribe/unsubscribe the right topics without recomputing them.
type statusWithSubnet struct {
	status      committeeSubscriptionStatus
	booleSubnet uint64
	alanSubnet  uint64
}

// p2pNetwork implements network.P2PNetwork
type p2pNetwork struct {
	parentCtx context.Context
	ctx       context.Context
	cancel    context.CancelFunc

	logger *zap.Logger
	cfg    *Config

	host            atomic.Pointer[host.Host]
	streamCtrl      streams.StreamController
	idx             peers.Index
	isIdxSet        atomic.Bool
	discoveryFailed atomic.Bool
	disc            discovery.Service
	topicsCtrl      topics.Controller
	msgRouter       network.MessageRouter
	msgResolver     topics.MsgPeersResolver
	msgValidator    validation.MessageValidator
	connHandler     connections.ConnHandler
	connGater       connmgrcore.ConnectionGater
	trustedPeers    []*peer.AddrInfo

	state int32
	// closeOnce makes Close() idempotent and safe to run on a partially-initialized network.
	closeOnce sync.Once

	// subscribedCommittees tracks committee subscription statuses for committees we've subscribed to.
	subscribedCommittees *hashmap.Map[string, statusWithSubnet]

	backoffConnector *backoffConnector

	// persistentSubnets holds subnets on node startup,
	// these subnets should not be unsubscribed from even if all validators associated with them are removed.
	//
	// Concurrency invariant: written only during single-threaded startup (SubscribeAll /
	// SubscribeRandoms / setup, all documented not-thread-safe and completed in startValidators
	// before the discovery/trim goroutines act on it). The goroutine readers
	// (subscribedSubnetsForCurrentEpoch) rely on that startup happens-before edge. If that ordering
	// ever changes, guard this field with a mutex (see TODO(convergence): -race-verified lock).
	persistentSubnets commons.Subnets
	// currentSubnets holds current subnets which depend on current active validators and committees
	currentSubnets   commons.Subnets
	currentSubnetsMu sync.RWMutex

	libConnManager connmgrcore.ConnManager

	nodeStorage       operatorstorage.Storage
	operatorDataStore operatordatastore.OperatorDataStore

	// discoveredPeersPool keeps track of recently discovered peers so we can rank them and choose
	// the best candidates to connect to.
	discoveredPeersPool *ttl.Map[peer.ID, discovery.DiscoveredPeer]
	// trimmedRecently keeps track of recently trimmed peers so we don't try to connect to these
	// shortly after we've trimmed these (we still might consider connecting to these once they
	// are removed from this map after some time passes)
	trimmedRecently *ttl.Map[peer.ID, struct{}]
}

var _ HealthChecker = (*p2pNetwork)(nil)

// New creates a new p2p network
func New(
	logger *zap.Logger,
	cfg *Config,
) (*p2pNetwork, error) {
	ctx, cancel := context.WithCancel(cfg.Ctx)

	n := &p2pNetwork{
		parentCtx:            cfg.Ctx,
		ctx:                  ctx,
		cancel:               cancel,
		logger:               logger.Named(log.NameP2PNetwork),
		cfg:                  cfg,
		msgRouter:            cfg.Router,
		msgValidator:         cfg.MessageValidator,
		state:                stateClosed,
		subscribedCommittees: hashmap.New[string, statusWithSubnet](),
		nodeStorage:          cfg.NodeStorage,
		operatorDataStore:    cfg.OperatorDataStore,
		discoveredPeersPool:  ttl.New[peer.ID, discovery.DiscoveredPeer](ctx, 30*time.Minute, 3*time.Minute),
		trimmedRecently:      ttl.New[peer.ID, struct{}](ctx, 30*time.Minute, 3*time.Minute),
	}
	if err := n.parseTrustedPeers(); err != nil {
		cancel() // release the ctx-bound ttl goroutines started above before bailing out
		return nil, err
	}
	return n, nil
}

func (n *p2pNetwork) parseTrustedPeers() error {
	if len(n.cfg.TrustedPeers) == 0 {
		return nil // No trusted peers to parse, return early
	}
	// Group addresses by peer ID.
	trustedPeers := map[peer.ID][]ma.Multiaddr{}
	for _, mas := range n.cfg.TrustedPeers {
		for ma := range strings.SplitSeq(mas, ",") {
			addrInfo, err := peer.AddrInfoFromString(ma)
			if err != nil {
				return fmt.Errorf("could not parse trusted peer: %w", err)
			}
			trustedPeers[addrInfo.ID] = append(trustedPeers[addrInfo.ID], addrInfo.Addrs...)
		}
	}
	for id, addrs := range trustedPeers {
		n.trustedPeers = append(n.trustedPeers, &peer.AddrInfo{ID: id, Addrs: addrs})
	}
	return nil
}

// Host implements HostProvider.
// Returns nil if the host has not yet been initialized (i.e. before SetupHost
// has stored it). The atomic load synchronizes-with the store in SetupHost,
// so callers can read the returned host safely from any goroutine — including
// the libp2p listener goroutines that fire connection-gater callbacks during
// SetupHost itself (see #2448).
func (n *p2pNetwork) Host() host.Host {
	if h := n.host.Load(); h != nil {
		return *h
	}
	return nil
}

// PeersIndex returns the peers index
func (n *p2pNetwork) PeersIndex() peers.Index {
	return n.idx
}

// Peers returns all peers we are connected to
func (n *p2pNetwork) Peers() []peer.ID {
	allPeers, err := n.topicsCtrl.Peers("")
	if err != nil {
		n.logger.Error("Cant list all peers", zap.Error(err))
		return nil
	}
	return allPeers
}

// PeersByTopic returns topic->peers mapping for all peers we are connected to
func (n *p2pNetwork) PeersByTopic() map[string][]peer.ID {
	tpcs := n.topicsCtrl.Topics()
	peerz := make(map[string][]peer.ID, len(tpcs))
	for _, tpc := range tpcs {
		peers, err := n.topicsCtrl.Peers(tpc)
		if err != nil {
			n.logger.Error("Cant get peers for specified topic", zap.String("topic", tpc), zap.Error(err))
			return nil
		}
		peerz[tpc] = peers
	}
	return peerz
}

// Close implements io.Closer. It is idempotent and safe on a network in any state —
// constructed-but-never-Setup, Setup-but-not-Started, Started, or Start-failed — so the owner can
// always defer Close() right after constructing the network. Idempotency is via closeOnce rather
// than the state field, because stateClosed is ambiguous: it's both the freshly-constructed state
// and the state Start() leaves on failure (where the host/services are allocated and still need
// closing).
func (n *p2pNetwork) Close() error {
	var err error
	n.closeOnce.Do(func() {
		atomic.SwapInt32(&n.state, stateClosing)
		defer atomic.StoreInt32(&n.state, stateClosed)

		n.cancel()

		// libConnManager/disc/idx/topicsCtrl/host are allocated only during Setup; guard each so a
		// network that was never Setup (or whose Setup failed partway) doesn't nil-panic on Close.
		if n.libConnManager != nil {
			if e := n.libConnManager.Close(); e != nil {
				n.logger.Warn("could not close connection manager", zap.Error(e))
			}
		}
		if n.disc != nil {
			if e := n.disc.Close(); e != nil {
				n.logger.Warn("could not close discovery", zap.Error(e))
			}
		}
		if n.idx != nil {
			if e := n.idx.Close(); e != nil {
				n.logger.Warn("could not close index", zap.Error(e))
			}
		}
		if n.topicsCtrl != nil {
			if e := n.topicsCtrl.Close(); e != nil {
				n.logger.Warn("could not close topics controller", zap.Error(e))
			}
		}
		if h := n.Host(); h != nil {
			err = h.Close()
		}
	})
	return err
}

func (n *p2pNetwork) getConnector() (chan peer.AddrInfo, error) {
	connector := make(chan peer.AddrInfo, connectorQueueSize)
	go func() {
		ctx, cancel := context.WithCancel(n.ctx)
		defer cancel()

		// Wait for own subnets to be subscribed to and updated.
		// TODO: wait more intelligently with a channel.
		select {
		case <-ctx.Done():
			return
		case <-time.After(8 * time.Second):
		}

		n.backoffConnector.Connect(ctx, connector)
	}()

	// Connect to trusted peers first.
	go func() {
		for _, addrInfo := range n.trustedPeers {
			connector <- *addrInfo
		}
	}()

	return connector, nil
}

// Start starts the discovery service, garbage collector (peer index), and reporting.
func (n *p2pNetwork) Start() (err error) {
	if atomic.SwapInt32(&n.state, stateReady) == stateReady {
		return fmt.Errorf("network already started")
	}
	defer func() {
		if err != nil {
			atomic.StoreInt32(&n.state, stateClosed)
		}
	}()

	host := n.Host()
	pAddrs, err := peer.AddrInfoToP2pAddrs(&peer.AddrInfo{
		ID:    host.ID(),
		Addrs: host.Addrs(),
	})
	if err != nil {
		return fmt.Errorf("resolve p2p address: %w", err)
	}
	maStrs := make([]string, len(pAddrs))
	for i, ima := range pAddrs {
		maStrs[i] = ima.String()
	}
	n.logger.Info("starting p2p",
		zap.String("my_address", strings.Join(maStrs, ",")),
		zap.Int("trusted_peers", len(n.trustedPeers)),
	)

	err = n.startDiscovery()
	if err != nil {
		return fmt.Errorf("could not start discovery: %w", err)
	}

	async.Interval(n.ctx, peersTrimmingInterval, n.peersTrimming())

	async.Interval(n.ctx, peersReportingInterval, recordPeerCount(n.ctx, n.logger, host))

	async.Interval(n.ctx, peerIdentitiesReportingInterval, recordPeerIdentities(n.ctx, host, n.idx))

	async.Interval(n.ctx, topicsReportingInterval, recordPeerCountPerTopic(n.ctx, n.logger, n.topicsCtrl))

	n.subscribeToFixedSubnets()

	return nil
}

// Returns a function that trims currently connected peers if necessary, namely:
//   - dropping peers with bad gossip score
//   - dropping irrelevant peers that don't have any subnet in common with us
//   - (when we are close to MaxPeers limit) dropping several peers with the worst score
//     which is based on how many valuable (dead/solo/duo) subnets a peer contributes
//   - (when Inbound peers are close to its limit) dropping several Inbound peers with
//     the worst score
func (n *p2pNetwork) peersTrimming() func() {
	return func() {
		ctx, cancel := context.WithTimeout(n.ctx, 60*time.Second)
		defer cancel()
		defer func() {
			_ = n.idx.GetSubnetsStats() // collect metrics
		}()

		hostNetwork := n.Host().Network()
		connMgr := peers.NewConnManager(n.logger, n.libConnManager, n.idx, n.idx)

		disconnectedCnt := connMgr.DisconnectFromBadPeers(hostNetwork, hostNetwork.Peers())
		if disconnectedCnt > 0 {
			// we can accept more peer connections now, no need to trim
			return
		}

		connectedPeers := hostNetwork.Peers()
		currentSubnets := n.currentSubnetsSnapshot()

		const maximumIrrelevantPeersToDisconnect = 3
		disconnectedCnt = connMgr.DisconnectFromIrrelevantPeers(
			maximumIrrelevantPeersToDisconnect,
			hostNetwork,
			connectedPeers,
			currentSubnets,
		)
		if disconnectedCnt > 0 {
			// we can accept more peer connections now, no need to trim
			return
		}

		// maxPeersToDrop value should be in the range of 3-5% of MaxPeers for trimming to work
		// fast enough so that our node finds good set of peers within 10-20 minutes after node
		// start; it shouldn't be too large because that would negatively affect Ethereum duty
		// execution quality
		const maxPeersToDrop = 4 // targeting MaxPeers in 60-90 range

		trimInboundOnly := false

		// see if we can accept more peer connections already (no need to trim), note we trim not
		// only when our current connections reach MaxPeers limit exactly but even if we get close
		// enough to it - this ensures we don't skip trim iteration because of "random fluctuations"
		// in currently connected peer count at that limit boundary
		connectedPeers = hostNetwork.Peers()
		if len(connectedPeers) <= n.cfg.MaxPeers-maxPeersToDrop {
			// We probably don't want to trim outgoing connections then, but from time-to-time we want to
			// trim (and rotate) some incoming connections when inbound limit is hit just to make sure
			// inbound connections are rotated occasionally in reliable manner.
			// Note, we don't want to trim incoming connections as often as outgoing connections (since
			// trimming outgoing connections often helps us discover valuable peers, while it's not really
			// the case with incoming connections - only slightly so) hence sometimes we randomly skip this
			in, _ := n.connectionStats()
			if in < n.inboundLimit() {
				return // skip trim iteration
			}
			if rand.Intn(5) > 0 { //nolint: gosec
				return // skip trim iteration
			}
			trimInboundOnly = true
		}

		inboundBefore, outboundBefore := n.connectionStats()
		peersToTrim := n.choosePeersToTrim(maxPeersToDrop, trimInboundOnly)
		if len(peersToTrim) == 0 {
			n.logger.Debug(
				"no peers selected for trimming",
				zap.Int("inbound_peers", inboundBefore),
				zap.Int("outbound_peers", outboundBefore),
				zap.Bool("trim_inbound_only", trimInboundOnly),
				zap.Int("trimmed_recently_size", n.trimmedRecently.SlowLen()),
			)
			return
		}
		connMgr.TrimPeers(ctx, hostNetwork, peersToTrim)
		for pid := range peersToTrim {
			n.trimmedRecently.Set(pid, struct{}{})
		}
		inboundAfter, outboundAfter := n.connectionStats()
		n.logger.Debug(
			"trimmed peers",
			zap.Int("inbound_peers_before_trim", inboundBefore),
			zap.Int("outbound_peers_before_trim", outboundBefore),
			zap.Int("inbound_peers_after_trim", inboundAfter),
			zap.Int("outbound_peers_after_trim", outboundAfter),
			zap.Bool("trim_inbound_only", trimInboundOnly),
			zap.Int("trimmed_recently_size", n.trimmedRecently.SlowLen()),
			zap.Any("trimmed_peers", maps.Keys(peersToTrim)),
		)
	}
}

// choosePeersToTrim returns a map of peers that are least-valuable to us based on how much
// (dead/solo/duo) they contribute to us.
func (n *p2pNetwork) choosePeersToTrim(trimCnt int, trimInboundOnly bool) map[peer.ID]struct{} {
	myPeers, err := n.topicsCtrl.Peers("")
	if err != nil {
		n.logger.Error("Cant get all of our peers", zap.Error(err))
		return nil
	}

	peerScores := n.buildPeerTrimScores(myPeers)
	slices.SortFunc(myPeers, func(a, b peer.ID) int {
		// sort in asc order (peers with the lowest scores come first)
		aScore := peerScores[a]
		bScore := peerScores[b]
		if aScore < bScore {
			return -1
		}
		if aScore > bScore {
			return 1
		}
		return 0
	})

	result := make(map[peer.ID]struct{}, trimCnt)
	ownSubnets := n.SubscribedSubnets()
	hostNetwork := n.Host().Network()
	for _, p := range myPeers {
		if trimCnt <= 0 {
			break
		}
		pConns := hostNetwork.ConnsToPeer(p)
		// we shouldn't have more than 1 connection per peer, but if we do we'd want a
		// warning about it logged, and we'd want to handle it to the best of our ability
		if len(pConns) > 1 {
			n.logger.Error(
				"choosePeersToTrim: encountered peer we have multiple open connections with (expected 1 at most)",
				zap.String("peer_id", p.String()),
				zap.Int("connections_count", len(pConns)),
			)
		}
		for _, pConn := range pConns {
			connDir := pConn.Stat().Direction
			if connDir == p2pnet.DirUnknown {
				n.logger.Error(
					"choosePeersToTrim: encountered peer connection with direction Unknown",
					zap.String("peer_id", p.String()),
				)
			}
			if connDir == p2pnet.DirOutbound && trimInboundOnly {
				continue
			}
			peerSubnets, _ := n.idx.GetPeerSubnets(p)
			sharedSubnets := ownSubnets.SharedSubnets(peerSubnets)
			n.logger.Debug("selected peer for trimming",
				fields.PeerID(p),
				zap.Float64("peer_score", peerScores[p]),
				zap.String("conn_direction", connDir.String()),
				zap.String("peer_subnets", peerSubnets.StringHumanReadable()),
				zap.Int("shared_subnets_count", len(sharedSubnets)),
			)
			result[p] = struct{}{}
			trimCnt--
		}
	}
	return result
}

// bootstrapDiscovery starts the required services
// it will try to bootstrap discovery service, and inject a connect function.
// the connect function checks if we can connect to the given peer and if so passing it to the backoff connector.
func (n *p2pNetwork) bootstrapDiscovery(connector chan peer.AddrInfo) {
	defer close(connector)
	err := tasks.Retry(func() error {
		return n.disc.Bootstrap(func(e discovery.PeerEvent) {
			if err := n.idx.CanConnect(e.AddrInfo.ID); err != nil {
				n.logger.Debug("skipping new peer", fields.PeerID(e.AddrInfo.ID), zap.Error(err))
				return
			}
			select {
			case connector <- e.AddrInfo:
			default:
				n.logger.Warn("connector queue is full, skipping new peer", fields.PeerID(e.AddrInfo.ID))
			}
		})
	}, 3)
	if err != nil {
		n.discoveryFailed.Store(true)
		n.logger.Error("could not setup discovery", zap.Error(err))
		return
	}
}

func (n *p2pNetwork) isReady() bool {
	return atomic.LoadInt32(&n.state) == stateReady
}

// Healthy reports whether the p2p network is operating normally.
// It satisfies the health-check interface from hprobe package.
func (n *p2pNetwork) Healthy(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !n.isReady() {
		return fmt.Errorf("p2p network not ready")
	}
	if n.discoveryFailed.Load() {
		return fmt.Errorf("discovery bootstrap failed")
	}
	return nil
}

// UpdateSubnets refreshes discovery records and pubsub subscriptions based on the current
// validator/committee set, including the Boole-fork transition window where both Alan and
// Boole topics may be active for the same subnet.
// NOTE: it won't perform the initial fixed-subnet subscriptions (use subscribeToFixedSubnets for that)
func (n *p2pNetwork) UpdateSubnets() {
	// TODO: this is a temporary fix to update subnets when validators are added/removed,
	// there is a pending PR to replace this: https://github.com/ssvlabs/ssv/pull/990
	ticker := time.NewTicker(time.Second)
	prevRegisteredSubnets := commons.Subnets{}
	prevRegisteredAlanSubnets := commons.Subnets{}
	prevRegisteredBooleSubnets := commons.Subnets{}
	defer ticker.Stop()

	// Run immediately and then every second.
	for {
		if n.ctx.Err() != nil {
			return
		}

		start := time.Now()

		alanSubnets, booleSubnets := n.subscribedSubnetsForCurrentEpoch()
		currentSubnets := unionSubnets(alanSubnets, booleSubnets)
		n.setCurrentSubnets(currentSubnets)

		// Compute the not yet registered subnets.
		addedSubnets := make([]uint64, 0)
		for _, subnet := range currentSubnets.SubnetList() {
			if !prevRegisteredSubnets.IsSet(subnet) {
				addedSubnets = append(addedSubnets, subnet)
			}
		}

		// Compute the not anymore registered subnets.
		removedSubnets := make([]uint64, 0)
		for _, subnet := range prevRegisteredSubnets.SubnetList() {
			if !currentSubnets.IsSet(subnet) {
				removedSubnets = append(removedSubnets, subnet)
			}
		}

		addedAlanSubnets, removedAlanSubnets := prevRegisteredAlanSubnets.DiffSubnets(alanSubnets)
		addedBooleSubnets, removedBooleSubnets := prevRegisteredBooleSubnets.DiffSubnets(booleSubnets)

		prevRegisteredSubnets = currentSubnets
		prevRegisteredAlanSubnets = alanSubnets
		prevRegisteredBooleSubnets = booleSubnets

		hasSubnetChanges := len(addedSubnets) > 0 || len(removedSubnets) > 0
		hasAlanChanges := addedAlanSubnets.HasActive() || removedAlanSubnets.HasActive()
		hasBooleChanges := addedBooleSubnets.HasActive() || removedBooleSubnets.HasActive()

		if hasSubnetChanges || hasAlanChanges || hasBooleChanges {
			n.idx.UpdateSelfRecord(func(self *records.NodeInfo) *records.NodeInfo {
				self.Metadata.Subnets = currentSubnets.StringHex()
				return self
			})

			var (
				errs                 error
				hasAdded, hasRemoved bool
			)
			if len(addedSubnets) > 0 {
				var err error
				hasAdded, err = n.disc.RegisterSubnets(addedSubnets...)
				if err != nil {
					n.logger.Debug("could not register subnets", zap.Error(err))
					errs = errors.Join(errs, err)
				}
			}
			if addedAlanSubnets.HasActive() {
				for _, addedSubnet := range addedAlanSubnets.SubnetList() {
					if err := n.subscribeSubnet(addedSubnet, false); err != nil {
						n.logger.Debug("could not subscribe to subnet", zap.Uint64("subnet", addedSubnet), zap.String("fork", "Alan"), zap.Error(err))
						errs = errors.Join(errs, err)
					} else {
						n.logger.Debug("subscribed to subnet", zap.Uint64("subnet", addedSubnet), zap.String("fork", "Alan"))
					}
				}
			}
			if addedBooleSubnets.HasActive() {
				for _, addedSubnet := range addedBooleSubnets.SubnetList() {
					if err := n.subscribeSubnet(addedSubnet, true); err != nil {
						n.logger.Debug("could not subscribe to subnet", zap.Uint64("subnet", addedSubnet), zap.String("fork", "Boole"), zap.Error(err))
						errs = errors.Join(errs, err)
					} else {
						n.logger.Debug("subscribed to subnet", zap.Uint64("subnet", addedSubnet), zap.String("fork", "Boole"))
					}
				}
			}
			if len(removedSubnets) > 0 {
				var err error
				hasRemoved, err = n.disc.DeregisterSubnets(removedSubnets...)
				if err != nil {
					n.logger.Debug("could not unregister subnets", zap.Error(err))
					errs = errors.Join(errs, err)
				}
			}
			if removedAlanSubnets.HasActive() {
				for _, removedSubnet := range removedAlanSubnets.SubnetList() {
					if err := n.unsubscribeSubnet(removedSubnet, false); err != nil {
						n.logger.Debug("could not unsubscribe from subnet", zap.Uint64("subnet", removedSubnet), zap.String("fork", "Alan"), zap.Error(err))
						errs = errors.Join(errs, err)
					} else {
						n.logger.Debug("unsubscribed from subnet", zap.Uint64("subnet", removedSubnet), zap.String("fork", "Alan"))
					}
				}
			}
			if removedBooleSubnets.HasActive() {
				for _, removedSubnet := range removedBooleSubnets.SubnetList() {
					if err := n.unsubscribeSubnet(removedSubnet, true); err != nil {
						n.logger.Debug("could not unsubscribe from subnet", zap.Uint64("subnet", removedSubnet), zap.String("fork", "Boole"), zap.Error(err))
						errs = errors.Join(errs, err)
					} else {
						n.logger.Debug("unsubscribed from subnet", zap.Uint64("subnet", removedSubnet), zap.String("fork", "Boole"))
					}
				}
			}
			if hasAdded || hasRemoved {
				go n.disc.PublishENR()
			}

			subnetsList := commons.AllSubnets.SharedSubnets(currentSubnets)
			n.logger.Debug("updated subnets",
				zap.Any("added", addedSubnets),
				zap.Any("removed", removedSubnets),
				zap.Any("subnets", subnetsList),
				zap.Any("subscribed_topics", n.topicsCtrl.Topics()),
				zap.Int("total_subnets", len(subnetsList)),
				fields.Took(time.Since(start)),
				zap.Error(errs),
			)
		}

		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (n *p2pNetwork) currentSubnetsSnapshot() commons.Subnets {
	n.currentSubnetsMu.RLock()
	defer n.currentSubnetsMu.RUnlock()

	return n.currentSubnets
}

func (n *p2pNetwork) setCurrentSubnets(subnets commons.Subnets) {
	n.currentSubnetsMu.Lock()
	defer n.currentSubnetsMu.Unlock()

	n.currentSubnets = subnets
}

// UpdateScoreParams updates the scoring parameters once per epoch through the call of n.topicsCtrl.UpdateScoreParams
func (n *p2pNetwork) UpdateScoreParams() {
	// TODO: this is a temporary solution to update the score parameters periodically.
	// But, we should use an appropriate trigger for the UpdateScoreParams function that should be
	// called once a validator is added or removed from the network

	// function to get the starting time of the next epoch
	nextEpochStartingTime := func() time.Time {
		currEpoch := n.cfg.NetworkConfig.EstimatedCurrentEpoch()
		nextEpoch := currEpoch + 1
		return n.cfg.NetworkConfig.EpochStartTime(nextEpoch)
	}

	timer := time.NewTimer(0)
	defer timer.Stop()

	// Run immediately and then once every epoch.
	for {
		select {
		case <-n.ctx.Done():
			return
		case <-timer.C:
		}

		err := n.topicsCtrl.UpdateScoreParams()
		if err != nil {
			n.logger.Debug("score parameters update failed", zap.Error(err))
		} else {
			n.logger.Debug("updated score parameters successfully")
		}

		// Reset to trigger on the beginning of the next epoch
		timer.Reset(time.Until(nextEpochStartingTime()))
	}
}

// getMaxPeers returns max peers of the given topic.
func (n *p2pNetwork) getMaxPeers(topic string) int {
	if len(topic) == 0 {
		return n.cfg.MaxPeers
	}
	return n.cfg.TopicMaxPeers
}

// buildPeerTrimScores snapshots topic membership once and computes trim scores
// for the given peers.
//
// The peer-scores are calculated based on:
//   - ownAlanSubnets / ownBooleSubnets: subnets we're subscribed to, per-fork.
//     During the Boole-fork transition both sets may be populated.
//   - ownSubnetPeers: our currently connected peer count per (fork, subnet) across
//     all peers in our topic mesh. Alan-N and Boole-N are tracked separately since
//     they are distinct gossipsub topics even though they share the subnet index.
//   - peerObservedSubnets: each peer's observed Alan/Boole participation from our
//     own topic mesh (precise, unlike the ENR bitfield).
//
// Algo:
//   - snapshot ownAlanSubnets, ownBooleSubnets, ownSubnetPeers, peerObservedSubnets
//   - for each candidate peer:
//   - build subnetPeersExcluding by decrementing the (fork, subnet) slots the peer actually serves
//   - Score the peer against the excluding-counts
func (n *p2pNetwork) buildPeerTrimScores(peerIDs []peer.ID) map[peer.ID]float64 {
	ownAlanSubnets, ownBooleSubnets := n.subscribedSubnetsForCurrentEpoch()
	ownSubnetPeers := newSubnetPeers()

	// peerPresence tracks a peer's observed Alan/Boole participation from our own
	// topic mesh. Unlike the ENR subnet bitfield (a union of Alan and Boole that
	// can't be disambiguated), this is precise — we know which topics we've seen
	// the peer on.
	type peerPresence struct {
		alan  commons.Subnets
		boole commons.Subnets
	}
	peerObservedSubnets := make(map[peer.ID]peerPresence)

	for topic, peers := range n.PeersByTopic() {
		subnet, isBoole, err := n.topicSubnet(topic)
		if err != nil {
			n.logger.Error("failed to convert topic to subnet", zap.String("topic", topic), zap.Error(err))
			continue
		}

		if isBoole {
			ownSubnetPeers.boole[subnet] = uint16(len(peers)) //nolint: gosec
		} else {
			ownSubnetPeers.alan[subnet] = uint16(len(peers)) //nolint: gosec
		}

		for _, peerID := range peers {
			presence := peerObservedSubnets[peerID]
			if isBoole {
				presence.boole.Set(subnet)
			} else {
				presence.alan.Set(subnet)
			}
			peerObservedSubnets[peerID] = presence
		}
	}

	scores := make(map[peer.ID]float64, len(peerIDs))
	for _, peerID := range peerIDs {
		presence := peerObservedSubnets[peerID]
		subnetPeersExcluding := ownSubnetPeers
		for subnet := uint64(0); subnet < commons.SubnetsCount; subnet++ {
			// Clamp to zero just in case the invariant "peer-observed implies count >= 1"
			// is ever broken by a future change.
			if presence.alan.IsSet(subnet) && subnetPeersExcluding.alan[subnet] >= 1 {
				subnetPeersExcluding.alan[subnet] -= 1
			}
			if presence.boole.IsSet(subnet) && subnetPeersExcluding.boole[subnet] >= 1 {
				subnetPeersExcluding.boole[subnet] -= 1
			}
		}

		scores[peerID] = subnetPeersExcluding.Score(ownAlanSubnets, ownBooleSubnets, presence.alan, presence.boole)
	}
	return scores
}

// topicSubnet parses a topic name as a subnet index, along with whether it's a Boole-fork topic.
func (n *p2pNetwork) topicSubnet(topic string) (subnet uint64, boole bool, err error) {
	subnet, boole, err = commons.ParseTopicSubnet(topic)
	if err != nil {
		return 0, false, fmt.Errorf("parse topic subnet: %w", err)
	}
	if subnet >= commons.SubnetsCount {
		return 0, false, fmt.Errorf("subnet must be in range [0, %d], got subnet: %d", commons.SubnetsCount, subnet)
	}
	return subnet, boole, nil
}

// SubnetPeers tracks peer counts per (fork, subnet) across our gossipsub topic mesh.
//
// During the Boole-fork transition the same subnet index (0..127) maps to two
// distinct gossipsub topics — Alan (ssv.v2.N) and Boole (/ssv/<net>/boole/N) —
// which have independent peer populations. Merging the counts (as a prior
// implementation did via `+=`) hides a dead Alan-N behind a healthy Boole-N (or
// vice versa) and misguides scoring. Tracking per-fork lets `Score` credit
// each (fork, subnet) we care about separately. After the transition the Alan
// side naturally stays zero for all new peers.
type SubnetPeers struct {
	alan  [commons.SubnetsCount]uint16
	boole [commons.SubnetsCount]uint16
}

func newSubnetPeers() SubnetPeers {
	return SubnetPeers{}
}

// newSubnetPeersFromPeerENR builds the optimistic contribution a newly-connected
// peer would make, given their advertised ENR subnet bitfield. The ENR is a union
// of the peer's Alan and Boole subnets (indistinguishable), so for each bit set
// we bump exactly the sides we are subscribed to — the peer might help with
// either or both, and we only ever track topics we actually care about.
func newSubnetPeersFromPeerENR(peerENR, ourAlan, ourBoole commons.Subnets) SubnetPeers {
	var result SubnetPeers
	for _, subnet := range peerENR.SubnetList() {
		if ourAlan.IsSet(subnet) {
			result.alan[subnet] = 1
		}
		if ourBoole.IsSet(subnet) {
			result.boole[subnet] = 1
		}
	}
	return result
}

func (a SubnetPeers) Add(b SubnetPeers) SubnetPeers {
	var sum SubnetPeers
	for i := range a.alan {
		sum.alan[i] = a.alan[i] + b.alan[i]
		sum.boole[i] = a.boole[i] + b.boole[i]
	}
	return sum
}

// Score estimates the value of a peer's (potential or observed) contribution to
// our topic mesh, summed across every (fork, subnet) pair we are subscribed to
// and the peer participates in.
//
// Parameters:
//   - ourAlan, ourBoole: subnets we are subscribed to, per-fork.
//   - theirAlan, theirBoole: the peer's participation per-fork.
//     For discovery (ENR-based scoring) pass the ENR bitfield for both sides —
//     a bit set in the ENR means the peer could be on Alan-N, Boole-N, or both,
//     so we credit every side we care about.
//     For trim scoring pass the peer's actually-observed Alan/Boole presence —
//     this credits the peer precisely for the topics they serve.
//
// For each (fork, subnet) pair we are subscribed to AND the peer participates in,
// the priority is based on how many other peers we have on that specific topic:
// dead (0) > solo (1) > duo (2) > healthy (3+). Summed across pairs.
func (a SubnetPeers) Score(ourAlan, ourBoole, theirAlan, theirBoole commons.Subnets) float64 {
	const (
		duoSubnetPriority  = 1
		soloSubnetPriority = 4
		deadSubnetPriority = 16
	)
	priority := func(count uint16) float64 {
		switch count {
		case 0:
			return deadSubnetPriority
		case 1:
			return soloSubnetPriority
		case 2:
			return duoSubnetPriority
		}
		return 0
	}

	score := float64(0)
	for subnet := uint64(0); subnet < commons.SubnetsCount; subnet++ {
		if ourAlan.IsSet(subnet) && theirAlan.IsSet(subnet) {
			score += priority(a.alan[subnet])
		}
		if ourBoole.IsSet(subnet) && theirBoole.IsSet(subnet) {
			score += priority(a.boole[subnet])
		}
	}
	return score
}

func (a SubnetPeers) String() string {
	var result strings.Builder
	for i := range a.alan {
		if a.alan[i] == 0 && a.boole[i] == 0 {
			continue
		}
		_, _ = fmt.Fprintf(&result, "%d:%d/%d ", i, a.alan[i], a.boole[i])
	}
	return strings.TrimSuffix(result.String(), " ")
}
