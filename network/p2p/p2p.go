package p2pv1

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	connmgrcore "github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/host"
	p2pnet "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	libp2pdiscbackoff "github.com/libp2p/go-libp2p/p2p/discovery/backoff"
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
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/utils/async"
	"github.com/ssvlabs/ssv/utils/hashmap"
	"github.com/ssvlabs/ssv/utils/retry"
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

const pinnedPeerTag = "pinned-peer"

// PeersIndexProvider holds peers index instance
type PeersIndexProvider interface {
	PeersIndex() peers.Index
}

// HostProvider holds host instance
type HostProvider interface {
	Host() host.Host
}

// p2pNetwork implements network.P2PNetwork
type p2pNetwork struct {
	parentCtx context.Context
	ctx       context.Context
	cancel    context.CancelFunc

	logger *zap.Logger
	cfg    *Config

	host         host.Host
	streamCtrl   streams.StreamController
	idx          peers.Index
	isIdxSet     atomic.Bool
	disc         discovery.Service
	topicsCtrl   topics.Controller
	msgRouter    network.MessageRouter
	msgResolver  topics.MsgPeersResolver
	msgValidator validation.MessageValidator
	connHandler  connections.ConnHandler
	connGater    connmgrcore.ConnectionGater
	// trustedPeers are priority peers we trust, but disconnection from them is possible
	trustedPeers []*peer.AddrInfo
	// pinnedPeers are peers that must stay connected: they are protected from pruning, persisted with Permanent TTL and redialed
	pinnedPeers *hashmap.Map[peer.ID, *peer.AddrInfo]

	state int32

	activeCommittees *hashmap.Map[string, validatorStatus]

	backoffConnector *libp2pdiscbackoff.BackoffConnector

	// persistentSubnets holds subnets on node startup,
	// these subnets should not be unsubscribed from even if all validators associated with them are removed
	persistentSubnets commons.Subnets
	// currentSubnets holds current subnets which depend on current active validators and committees
	currentSubnets commons.Subnets

	libConnManager connmgrcore.ConnManager

	nodeStorage             operatorstorage.Storage
	operatorPKHashToPKCache *hashmap.Map[string, []byte] // used for metrics
	operatorSigner          keys.OperatorSigner
	operatorDataStore       operatordatastore.OperatorDataStore

	// discoveredPeersPool keeps track of recently discovered peers so we can rank them and choose
	// the best candidates to connect to.
	discoveredPeersPool *ttl.Map[peer.ID, discovery.DiscoveredPeer]
	// trimmedRecently keeps track of recently trimmed peers so we don't try to connect to these
	// shortly after we've trimmed these (we still might consider connecting to these once they
	// are removed from this map after some time passes)
	trimmedRecently *ttl.Map[peer.ID, struct{}]

	// backoff scheduler for pinned peer redials
	pinnedRedialScheduler *retry.Scheduler
	// stabilization timers per pinned peer: reset backoff only if connection
	// survives this window without a disconnect.
	pinnedStabilizers *hashmap.Map[peer.ID, *time.Timer]
}

// New creates a new p2p network
func New(
	logger *zap.Logger,
	cfg *Config,
) (*p2pNetwork, error) {
	ctx, cancel := context.WithCancel(cfg.Ctx)

	n := &p2pNetwork{
		parentCtx:               cfg.Ctx,
		ctx:                     ctx,
		cancel:                  cancel,
		logger:                  logger.Named(log.NameP2PNetwork),
		cfg:                     cfg,
		msgRouter:               cfg.Router,
		msgValidator:            cfg.MessageValidator,
		state:                   stateClosed,
		activeCommittees:        hashmap.New[string, validatorStatus](),
		nodeStorage:             cfg.NodeStorage,
		operatorPKHashToPKCache: hashmap.New[string, []byte](),
		operatorSigner:          cfg.OperatorSigner,
		operatorDataStore:       cfg.OperatorDataStore,
		discoveredPeersPool:     ttl.New[peer.ID, discovery.DiscoveredPeer](30*time.Minute, 3*time.Minute),
		trimmedRecently:         ttl.New[peer.ID, struct{}](30*time.Minute, 3*time.Minute),
		pinnedPeers:             hashmap.New[peer.ID, *peer.AddrInfo](),
		pinnedRedialScheduler: retry.NewScheduler(retry.BackoffConfig{
			Initial: pinnedRedialInitialBackoff,
			Max:     pinnedRedialMaxBackoff,
			Jitter:  time.Second,
		}),
		pinnedStabilizers: hashmap.New[peer.ID, *time.Timer](),
	}

	if err := n.parsePinnedPeers(); err != nil {
		return nil, err
	}
	if err := n.parseTrustedPeers(); err != nil {
		return nil, err
	}
	return n, nil
}

func (n *p2pNetwork) parseTrustedPeers() error {
	if len(n.cfg.TrustedPeers) == 0 {
		return nil // No trusted peers to parse, return early
	}
	grouped := parsePeerList(n.cfg.TrustedPeers, n.logger, "trusted")
	for id, addrs := range grouped {
		n.trustedPeers = append(n.trustedPeers, &peer.AddrInfo{ID: id, Addrs: addrs})
	}
	return nil
}

// parsePinnedPeers parses the configured PinnedPeers and groups addresses by peer ID.
// Supports YAML list or comma-separated entries, and multiaddrs containing /p2p/<id>.
func (n *p2pNetwork) parsePinnedPeers() error {
	if len(n.cfg.PinnedPeers) == 0 {
		return nil
	}
	grouped := parsePeerList(n.cfg.PinnedPeers, n.logger, "pinned")
	for id, addrs := range grouped {
		if len(addrs) == 0 {
			n.logger.Warn("ignoring pinned peer without address (full multiaddr required)", fields.PeerID(id))
			continue
		}
		ai := &peer.AddrInfo{ID: id, Addrs: addrs}
		n.pinnedPeers.Set(id, ai)
	}
	return nil
}

func (n *p2pNetwork) isPinned(id peer.ID) bool { return n.pinnedPeers.Has(id) }

func (n *p2pNetwork) filterUnpinned(ids []peer.ID) []peer.ID {
	if len(ids) == 0 {
		// nothing to filter
		return ids
	}
	out := make([]peer.ID, 0, len(ids))
	for _, id := range ids {
		if !n.isPinned(id) {
			out = append(out, id)
		}
	}
	return out
}

// onPinnedPeerDiscovered updates pinned peer addresses and protection when a discovery event for it arrives.
func (n *p2pNetwork) onPinnedPeerDiscovered(ai peer.AddrInfo) {
	if !n.isPinned(ai.ID) || n.host == nil {
		return
	}
	// Merge new addresses into pinned map and peerstore with permanent TTL.
	if len(ai.Addrs) > 0 {
		if existing, ok := n.pinnedPeers.Get(ai.ID); ok && existing != nil {
			// make an updated copy instead of mutating to prevent concurrency issues
			existing = &peer.AddrInfo{
				ID:    existing.ID,
				Addrs: append(existing.Addrs, ai.Addrs...),
			}
			n.pinnedPeers.Set(ai.ID, existing)
		} else {
			n.pinnedPeers.Set(ai.ID, &peer.AddrInfo{ID: ai.ID, Addrs: ai.Addrs})
		}
		n.host.Peerstore().AddAddrs(ai.ID, ai.Addrs, peerstore.PermanentAddrTTL)
		n.logger.Info("updated pinned peer addrs via discovery", fields.PeerID(ai.ID), zap.Int("new_addrs", len(ai.Addrs)))
	}
	if n.libConnManager != nil {
		n.libConnManager.Protect(ai.ID, pinnedPeerTag)
	}
}

// parsePeerList parses a list of strings that may be comma-separated groups
// or YAML list entries of full multiaddrs. It logs and skips
// invalid entries and returns a map of peer.ID to the union of provided addrs.
func parsePeerList(entries []string, logger *zap.Logger, kind string) map[peer.ID][]ma.Multiaddr {
	grouped := make(map[peer.ID][]ma.Multiaddr)
	for _, listStr := range entries {
		for _, entry := range strings.Split(listStr, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			ai, err := peer.AddrInfoFromString(entry)
			if err != nil {
				if logger != nil {
					logger.Warn("could not parse peer entry", zap.String("kind", kind), zap.String("entry", entry), zap.Error(err))
				}
				continue
			}
			grouped[ai.ID] = append(grouped[ai.ID], ai.Addrs...)
		}
	}
	return grouped
}

// Host implements HostProvider
func (n *p2pNetwork) Host() host.Host {
	return n.host
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

// Close implements io.Closer
func (n *p2pNetwork) Close() error {
	atomic.SwapInt32(&n.state, stateClosing)
	defer atomic.StoreInt32(&n.state, stateClosed)
	n.cancel()
	if n.pinnedRedialScheduler != nil {
		n.pinnedRedialScheduler.Close()
	}
	if n.pinnedStabilizers != nil {
		n.pinnedStabilizers.Range(func(_ peer.ID, t *time.Timer) bool {
			if t != nil {
				t.Stop()
			}
			return true
		})
	}
	if err := n.libConnManager.Close(); err != nil {
		n.logger.Warn("could not close discovery", zap.Error(err))
	}
	if err := n.disc.Close(); err != nil {
		n.logger.Warn("could not close discovery", zap.Error(err))
	}
	if err := n.idx.Close(); err != nil {
		n.logger.Warn("could not close index", zap.Error(err))
	}
	if err := n.topicsCtrl.Close(); err != nil {
		n.logger.Warn("could not close topics controller", zap.Error(err))
	}
	return n.host.Close()
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

	// Connect to pinned and trusted peers first.
	go func() {
		// Pinned peers: enqueue configured pinned peers.
		pinned := make([]*peer.AddrInfo, 0)
		n.pinnedPeers.Range(func(_ peer.ID, ai *peer.AddrInfo) bool {
			if ai != nil {
				pinned = append(pinned, ai)
			}
			return true
		})
		for _, ai := range pinned {
			if ai == nil {
				continue
			}
			connector <- *ai
		}
		// Trusted peers: attempt to resolve addresses if missing, then enqueue.
		for _, addrInfo := range n.trustedPeers {
			connector <- *addrInfo
		}
	}()

	return connector, nil
}

// PinnedPeersProvider exposes runtime management for pinned peers.
type PinnedPeersProvider interface {
	ListPinned() []peer.AddrInfo
	PinPeer(peer.AddrInfo) error
	UnpinPeer(peer.ID) error
}

// Ensure p2pNetwork implements PinnedPeersProvider
var _ PinnedPeersProvider = (*p2pNetwork)(nil)

// ListPinned returns a snapshot of pinned peers with their addresses.
func (n *p2pNetwork) ListPinned() []peer.AddrInfo {
	out := make([]peer.AddrInfo, 0)
	n.pinnedPeers.Range(func(_ peer.ID, ai *peer.AddrInfo) bool {
		if ai != nil {
			cp := peer.AddrInfo{ID: ai.ID, Addrs: append([]ma.Multiaddr(nil), ai.Addrs...)}
			out = append(out, cp)
		}
		return true
	})
	return out
}

// PinPeer adds or updates a pinned peer, protects it, persists addresses and attempts to connect.
func (n *p2pNetwork) PinPeer(ai peer.AddrInfo) error {
	if ai.ID == "" {
		return fmt.Errorf("empty peer id")
	}
	if len(ai.Addrs) == 0 {
		return fmt.Errorf("pinned peer requires full multiaddr with address")
	}
	if n.host == nil {
		return fmt.Errorf("p2p host is not initialized")
	}
	// Merge into map
	if existing, ok := n.pinnedPeers.Get(ai.ID); ok && existing != nil {
		// make an updated copy instead of mutating to prevent concurrency issues
		existing = &peer.AddrInfo{
			ID:    existing.ID,
			Addrs: append(existing.Addrs, ai.Addrs...),
		}
		n.pinnedPeers.Set(ai.ID, existing)
	} else {
		// ensure we store even with no addresses
		n.pinnedPeers.Set(ai.ID, &peer.AddrInfo{ID: ai.ID, Addrs: append([]ma.Multiaddr(nil), ai.Addrs...)})
	}

	// Persist addresses & protect
	n.host.Peerstore().AddAddrs(ai.ID, ai.Addrs, peerstore.PermanentAddrTTL)
	if n.libConnManager != nil {
		n.libConnManager.Protect(ai.ID, pinnedPeerTag)
	}
	// Attempt connect; if not connected, schedule redial with backoff.
	go func(ai peer.AddrInfo) {
		ctx, cancel := context.WithTimeout(n.ctx, 20*time.Second)
		defer cancel()
		_ = n.host.Connect(ctx, ai)
		if n.host.Network().Connectedness(ai.ID) != p2pnet.Connected {
			n.schedulePinnedConnect(ai.ID)
		}
	}(ai)
	return nil
}

// schedulePinnedConnect schedules redial attempts for a pinned peer until the
// peer becomes connected, using the configured backoff policy.
func (n *p2pNetwork) schedulePinnedConnect(id peer.ID) {
	n.pinnedRedialScheduler.Schedule(id.String(), func(attempt int) bool {
		n.logger.Info("redialing pinned peer", fields.PeerID(id), zap.Int("attempt", attempt))
		ctx2, cancel2 := context.WithTimeout(n.ctx, 20*time.Second)
		defer cancel2()
		if err := n.host.Connect(ctx2, peer.AddrInfo{ID: id}); err != nil {
			n.logger.Debug("pinned peer connect failed", fields.PeerID(id), zap.Error(err))
		}
		return n.host.Network().Connectedness(id) == p2pnet.Connected
	})
}

// UnpinPeer removes a pinned peer and cancels its protection/backoff.
func (n *p2pNetwork) UnpinPeer(id peer.ID) error {
	if id == "" {
		return fmt.Errorf("empty peer id")
	}
	if !n.pinnedPeers.Delete(id) {
		return fmt.Errorf("peer not pinned")
	}

	if n.libConnManager != nil {
		n.libConnManager.Unprotect(id, pinnedPeerTag)
	}
	// Cancel any scheduled redial
	n.pinnedRedialScheduler.Cancel(id.String())
	// Cancel stabilization timer if present
	if t, ok := n.pinnedStabilizers.Get(id); ok && t != nil {
		t.Stop()
		n.pinnedStabilizers.Delete(id)
	}
	return nil
}

// Start starts the discovery service, garbage collector (peer index), and reporting.
func (n *p2pNetwork) Start() error {
	if atomic.SwapInt32(&n.state, stateReady) == stateReady {
		// return errors.New("could not setup network: in ready state")
		return nil
	}

	pAddrs, err := peer.AddrInfoToP2pAddrs(&peer.AddrInfo{
		ID:    n.host.ID(),
		Addrs: n.host.Addrs(),
	})
	if err != nil {
		n.logger.Fatal("could not get my address", zap.Error(err))
	}
	maStrs := make([]string, len(pAddrs))
	for i, ima := range pAddrs {
		maStrs[i] = ima.String()
	}
	pinnedCount := n.pinnedPeers.SlowLen()
	n.logger.Info("starting p2p",
		zap.String("my_address", strings.Join(maStrs, ",")),
		zap.Int("trusted_peers", len(n.trustedPeers)),
		zap.Int("pinned_peers", pinnedCount),
	)

	err = n.startDiscovery()
	if err != nil {
		return fmt.Errorf("could not start discovery: %w", err)
	}

	async.Interval(n.ctx, peersTrimmingInterval, n.peersTrimming())

	async.Interval(n.ctx, peersReportingInterval, recordPeerCount(n.ctx, n.logger, n.host))

	async.Interval(n.ctx, peerIdentitiesReportingInterval, recordPeerIdentities(n.ctx, n.host, n.idx))

	async.Interval(n.ctx, topicsReportingInterval, recordPeerCountPerTopic(n.ctx, n.logger, n.topicsCtrl, 2))

	if err := n.subscribeToFixedSubnets(); err != nil {
		return err
	}

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

		connMgr := peers.NewConnManager(n.logger, n.libConnManager, n.idx, n.idx)

		// Never disconnect pinned peers when dropping bad peers.
		disconnectedCnt := connMgr.DisconnectFromBadPeers(n.host.Network(), n.filterUnpinned(n.host.Network().Peers()))
		if disconnectedCnt > 0 {
			// we can accept more peer connections now, no need to trim
			return
		}

		connectedPeers := n.filterUnpinned(n.host.Network().Peers())

		const maximumIrrelevantPeersToDisconnect = 3
		disconnectedCnt = connMgr.DisconnectFromIrrelevantPeers(
			maximumIrrelevantPeersToDisconnect,
			n.host.Network(),
			connectedPeers,
			n.currentSubnets,
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
		connectedPeers = n.filterUnpinned(n.host.Network().Peers())
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
		connMgr.TrimPeers(ctx, n.host.Network(), peersToTrim)
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
			zap.Any("trimmed_peers", maps.Keys(peersToTrim)),
		)
	}
}

// choosePeersToTrim returns a map of peers that are least-valuable to us based on how much
// (dead/solo/duo) they contribute to us (as defined by peerScore func).
func (n *p2pNetwork) choosePeersToTrim(trimCnt int, trimInboundOnly bool) map[peer.ID]struct{} {
	myPeers, err := n.topicsCtrl.Peers("")
	if err != nil {
		n.logger.Error("Cant get all of our peers", zap.Error(err))
		return nil
	}

	// Note: a pinned peer might not be in pubsub, but if it is, we must not trim it.
	myPeers = n.filterUnpinned(myPeers)

	slices.SortFunc(myPeers, func(a, b peer.ID) int {
		// sort in asc order (peers with the lowest scores come first)
		aScore := n.peerScore(a)
		bScore := n.peerScore(b)
		if aScore < bScore {
			return -1
		}
		if aScore > bScore {
			return 1
		}
		return 0
	})

	result := make(map[peer.ID]struct{}, trimCnt)
	for _, p := range myPeers {
		if trimCnt <= 0 {
			break
		}
		pConns := n.host.Network().ConnsToPeer(p)
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
	err := tasks.Retry(func() error {
		return n.disc.Bootstrap(func(e discovery.PeerEvent) {
			if err := n.idx.CanConnect(e.AddrInfo.ID); err != nil {
				n.logger.Debug("skipping new peer", fields.PeerID(e.AddrInfo.ID), zap.Error(err))
				return
			}
			// If this is a pinned peer, persist/refresh its addrs and ensure protection.
			n.onPinnedPeerDiscovered(e.AddrInfo)
			select {
			case connector <- e.AddrInfo:
			default:
				n.logger.Warn("connector queue is full, skipping new peer", fields.PeerID(e.AddrInfo.ID))
			}
		})
	}, 3)
	if err != nil {
		n.logger.Panic("could not setup discovery", zap.Error(err))
	}
}

func (n *p2pNetwork) isReady() bool {
	return atomic.LoadInt32(&n.state) == stateReady
}

// UpdateSubnets will update the registered subnets according to active validators
// NOTE: it won't subscribe to the subnets (use subscribeToFixedSubnets for that)
func (n *p2pNetwork) UpdateSubnets() {
	// TODO: this is a temporary fix to update subnets when validators are added/removed,
	// there is a pending PR to replace this: https://github.com/ssvlabs/ssv/pull/990
	ticker := time.NewTicker(time.Second)
	registeredSubnets := commons.Subnets{}
	defer ticker.Stop()

	// Run immediately and then every second.
	for ; true; <-ticker.C {
		start := time.Now()

		updatedSubnets := n.SubscribedSubnets()
		n.currentSubnets = updatedSubnets

		// Compute the not yet registered subnets.
		addedSubnets := make([]uint64, 0)
		subnetList := updatedSubnets.SubnetList()
		for _, subnet := range subnetList {
			if !registeredSubnets.IsSet(subnet) {
				addedSubnets = append(addedSubnets, subnet)
			}
		}

		// Compute the not anymore registered subnets.
		removedSubnets := make([]uint64, 0)
		subnetList = registeredSubnets.SubnetList()
		for _, subnet := range subnetList {
			if !updatedSubnets.IsSet(subnet) {
				removedSubnets = append(removedSubnets, subnet)
			}
		}

		registeredSubnets = updatedSubnets

		if len(addedSubnets) == 0 && len(removedSubnets) == 0 {
			continue
		}

		n.idx.UpdateSelfRecord(func(self *records.NodeInfo) *records.NodeInfo {
			self.Metadata.Subnets = n.currentSubnets.String()
			return self
		})

		// Register/unregister subnets for discovery.
		var errs error
		var hasAdded, hasRemoved bool
		if len(addedSubnets) > 0 {
			var err error
			hasAdded, err = n.disc.RegisterSubnets(addedSubnets...)
			if err != nil {
				n.logger.Debug("could not register subnets", zap.Error(err))
				errs = errors.Join(errs, err)
			}
		}
		if len(removedSubnets) > 0 {
			var err error
			hasRemoved, err = n.disc.DeregisterSubnets(removedSubnets...)
			if err != nil {
				n.logger.Debug("could not unregister subnets", zap.Error(err))
				errs = errors.Join(errs, err)
			}

			// Unsubscribe from the removed subnets.
			for _, removedSubnet := range removedSubnets {
				if err := n.unsubscribeSubnet(removedSubnet); err != nil {
					n.logger.Debug("could not unsubscribe from subnet", zap.Uint64("subnet", removedSubnet), zap.Error(err))
					errs = errors.Join(errs, err)
				} else {
					n.logger.Debug("unsubscribed from subnet", zap.Uint64("subnet", removedSubnet))
				}
			}
		}
		if hasAdded || hasRemoved {
			go n.disc.PublishENR()
		}

		subnetsList := commons.AllSubnets.SharedSubnets(n.currentSubnets)
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

	// Create timer that triggers on the beginning of the next epoch
	timer := time.NewTimer(time.Until(nextEpochStartingTime()))
	defer timer.Stop()

	// Run immediately and then once every epoch
	for ; true; <-timer.C {
		// Update score parameters
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

// peerScore calculates peer score based on how valuable this peer would have been if we didn't
// have him, but then connected with.
func (n *p2pNetwork) peerScore(peerID peer.ID) float64 {
	// Compute number of peers we're connected to for each subnet excluding peer with peerID.
	subnetPeersExcluding := SubnetPeers{}
	for topic, peers := range n.PeersByTopic() {
		subnet, err := strconv.ParseInt(commons.GetTopicBaseName(topic), 10, 64)
		if err != nil {
			n.logger.Error("failed to parse topic",
				zap.String("topic", topic), zap.Error(err))
			continue
		}
		if subnet < 0 || subnet >= commons.SubnetsCount {
			n.logger.Error("invalid topic",
				zap.String("topic", topic), zap.Int("subnet", int(subnet)))
			continue
		}
		for _, pID := range peers {
			if pID == peerID {
				continue
			}
			subnetPeersExcluding[subnet]++
		}
	}

	ownSubnets := n.SubscribedSubnets()
	peerSubnets, _ := n.PeersIndex().GetPeerSubnets(peerID)
	return subnetPeersExcluding.Score(ownSubnets, peerSubnets)
}

// SubnetPeers contains the number of peers we are connected to for each subnet.
type SubnetPeers [commons.SubnetsCount]uint16

func (a SubnetPeers) Add(b SubnetPeers) SubnetPeers {
	var sum SubnetPeers
	for i := range a {
		sum[i] = a[i] + b[i]
	}
	return sum
}

// Score estimates how many valuable subnets the given peer would contribute.
// Param ours defines subnets we are interested in.
// Param theirs defines subnets given peer has to offer.
func (a SubnetPeers) Score(ours, theirs commons.Subnets) float64 {
	const (
		duoSubnetPriority  = 1
		soloSubnetPriority = 4
		deadSubnetPriority = 16
	)
	score := float64(0)

	for i := range a {
		// #nosec G115 -- subnet index is never negative
		if ours.IsSet(uint64(i)) && theirs.IsSet(uint64(i)) {
			switch a[i] {
			case 0:
				score += deadSubnetPriority
			case 1:
				score += soloSubnetPriority
			case 2:
				score += duoSubnetPriority
			}
		}
	}
	return score
}

func (a SubnetPeers) String() string {
	var b strings.Builder
	for i, v := range a {
		if v > 0 {
			_, _ = fmt.Fprintf(&b, "%d:%d ", i, v)
		}
	}
	return b.String()
}
