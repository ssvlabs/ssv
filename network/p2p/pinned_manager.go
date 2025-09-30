package p2pv1

import (
	"context"
	"fmt"
	"time"

	"slices"
	"strings"

	connmgrcore "github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/host"
	p2pnet "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	ma "github.com/multiformats/go-multiaddr"
	"go.uber.org/zap"

	p2pcommons "github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/utils/hashmap"
	"github.com/ssvlabs/ssv/utils/retry"
)

const (
	defaultInitialBackoff = 10 * time.Second
	defaultMaxBackoff     = 10 * time.Minute
	pinnedPeerTag         = "pinned-peer"
	pinnedConnectTimeout  = 20 * time.Second
	// stabilization window: connection must survive this long before we reset backoff
	pinnedStabilizeWindow = 30 * time.Second
)

// pinnedPeersManager coordinates the full lifecycle of "pinned" peers — peers
// the node should stay connected to, at all times.
//
// Responsibilities
//   - Stores the set of pinned peers together with their known multiaddrs.
//   - Persists pinned peer addresses into the libp2p peerstore and applies a
//     connection-manager protection tag so they are not trimmed by policy.
//   - Maintains connection liveness for pinned peers by scheduling reconnects
//     with exponential backoff when they disconnect, and by resetting that backoff
//     after a connection survives a stabilization window.
//   - Reacts to network discovery by merging newly learned addresses for already
//     pinned peers and persisting them.
//
// Correct usage
//  1. Create the manager early (before the libp2p host is available):
//     m := newPinnedPeersManager(ctx, logger)
//  2. Seed from configuration (optional, pre-attach only):
//     m.seedPinsFromConfig([]peer.AddrInfo{ ... })
//     seedPinsFromConfig expects full multiaddrs (including /p2p/<peerID>) and
//     may be called multiple times before AttachHost. Calling it after attach
//     panics; use PinPeer at runtime instead.
//  3. Attach the libp2p host exactly once:
//     m.AttachHost(host, host.ConnManager())
//     AttachHost persists addresses, protects pinned peers, schedules reconnects
//     for any not yet-connected peers, and registers network notifications used
//     to stop and resume backoff when peers connect/disconnect. It panics if the
//     host or conn manager are nil, or if called more than once.
//  4. Runtime operations (post-attach only):
//     - PinPeer(ai): merges addresses for ai.ID, persists/protects, and starts an
//     immediate connect attempt; if the peer remains disconnected it continues
//     with backoff.
//     - UnpinPeer(id): removes the peer from the pinned set, unprotects it, clears
//     addresses from the peerstore and cancels any pending reconnects/timers.
//     Existing live connections are not force-closed; they are simply no longer
//     managed by the pinned policy.
//     - onDiscovered(ai): when discovery yields addresses for an already pinned
//     peer, merges and persists them. No-op before attach or for non‑pinned IDs.
//  5. close(): cancels internal work (connect attempts, backoff/timers) and
//     unregisters network notifications. It is safe to call even if AttachHost
//     was never called.
//
// All public methods are safe to call from multiple goroutines. The manager is
// typically owned by the p2p network component and not used directly elsewhere.
type pinnedPeersManager struct {
	ctx    context.Context
	cancel context.CancelFunc
	logger *zap.Logger

	host    host.Host
	connMgr connmgrcore.ConnManager

	peers     *hashmap.Map[peer.ID, *peer.AddrInfo]
	keepalive *keepalive
	notifiee  p2pnet.Notifiee
}

func newPinnedPeersManager(ctx context.Context, logger *zap.Logger) *pinnedPeersManager {
	// derive a cancelable context so Close() can stop in-flight work
	cctx, cancel := context.WithCancel(ctx)
	m := &pinnedPeersManager{
		ctx:       cctx,
		cancel:    cancel,
		logger:    logger.Named("PinnedPeers"),
		peers:     hashmap.New[peer.ID, *peer.AddrInfo](),
		keepalive: nil,
	}
	return m
}

func (m *pinnedPeersManager) close() {
	// cancel the context to stop in-flight Connect calls, scheduled retries, etc.
	m.cancel()
	if m.host != nil && m.notifiee != nil {
		m.host.Network().StopNotify(m.notifiee)
		m.notifiee = nil
	}
	if m.keepalive != nil {
		m.keepalive.close()
	}
}

// mergePinned merges the given AddrInfo into the in-memory pinned set
// and returns the resulting AddrInfo (with deduplicated addrs).
func (m *pinnedPeersManager) mergePinned(ai peer.AddrInfo) peer.AddrInfo {
	if existing, ok := m.peers.Get(ai.ID); ok {
		merged := peer.AddrInfo{ID: existing.ID, Addrs: p2pcommons.DedupMultiaddrs(append(existing.Addrs, ai.Addrs...))}
		m.peers.Set(ai.ID, &merged)
		return merged
	}
	fresh := peer.AddrInfo{ID: ai.ID, Addrs: p2pcommons.DedupMultiaddrs(ai.Addrs)}
	m.peers.Set(ai.ID, &fresh)
	return fresh
}

// addToPeerStoreAndProtect stores addrs in the peerstore (if any) and protects the peer via conn manager.
func (m *pinnedPeersManager) addToPeerStoreAndProtect(ai peer.AddrInfo) {
	if len(ai.Addrs) > 0 {
		m.host.Peerstore().AddAddrs(ai.ID, ai.Addrs, peerstore.PermanentAddrTTL)
	}
	m.connMgr.Protect(ai.ID, pinnedPeerTag)
}

// seedPinsFromConfig initializes the manager with a list of pinned peers. It should be
// called during setup, before AttachHost. Panics if called after host is attached
// to avoid surprising late mutations of protection and redial hooks.
func (m *pinnedPeersManager) seedPinsFromConfig(pins []peer.AddrInfo) {
	if m.host != nil {
		panic("seedPinsFromConfig must be called before AttachHost, use PinPeer instead.")
	}
	for _, ai := range pins {
		if ai.ID == "" || len(ai.Addrs) == 0 {
			continue
		}
		m.peers.Set(ai.ID, &peer.AddrInfo{ID: ai.ID, Addrs: p2pcommons.DedupMultiaddrs(ai.Addrs)})
	}
}

// attachHost wires the manager to a libp2p host/connmgr; persists addrs, protects peers, and registers redial hooks.
// Panics if called with nil host or conn manager.
func (m *pinnedPeersManager) attachHost(h host.Host, cm connmgrcore.ConnManager) {
	if h == nil || cm == nil {
		panic("AttachHost requires non-nil host and conn manager")
	}
	if m.host != nil {
		panic("AttachHost called more than once")
	}
	m.host = h
	m.connMgr = cm
	// initialize keepalive now that connect primitives are available
	m.keepalive = newKeepalive(
		m.ctx,
		m.logger,
		m.isPinned,
		func(ctx context.Context, ai peer.AddrInfo) error { return m.host.Connect(ctx, ai) },
		func(id peer.ID) bool { return m.host.Network().Connectedness(id) == p2pnet.Connected },
	)
	// Persist addrs & protect; kick off redial if not yet connected.
	m.peers.Range(func(_ peer.ID, ai *peer.AddrInfo) bool {
		m.addToPeerStoreAndProtect(*ai)
		m.keepalive.enqueue(*ai, false)
		return true
	})

	// Redial on disconnect; stop attempts on connect and start stabilization window.
	nb := &p2pnet.NotifyBundle{
		ConnectedF:    func(_ p2pnet.Network, c p2pnet.Conn) { m.keepalive.onConnected(c.RemotePeer()) },
		DisconnectedF: func(_ p2pnet.Network, c p2pnet.Conn) { m.keepalive.onDisconnected(c.RemotePeer()) },
	}
	m.notifiee = nb
	m.host.Network().Notify(nb)
}

func (m *pinnedPeersManager) isPinned(id peer.ID) bool { return m.peers.Has(id) }

// isHostAttached reports whether AttachHost has been successfully called.
func (m *pinnedPeersManager) isHostAttached() bool { return m.host != nil && m.connMgr != nil }

// filterUnpinned returns a new slice containing only peers that are not pinned.
func (m *pinnedPeersManager) filterUnpinned(ids []peer.ID) []peer.ID {
	if len(ids) == 0 {
		return ids
	}
	out := make([]peer.ID, 0, len(ids))
	for _, id := range ids {
		if !m.isPinned(id) {
			out = append(out, id)
		}
	}
	return out
}

// ListPinned returns a copy snapshot.
func (m *pinnedPeersManager) ListPinned() []peer.AddrInfo {
	out := make([]peer.AddrInfo, 0)
	m.peers.Range(func(_ peer.ID, ai *peer.AddrInfo) bool {
		cp := peer.AddrInfo{ID: ai.ID, Addrs: append([]ma.Multiaddr(nil), ai.Addrs...)}
		out = append(out, cp)
		return true
	})
	// deterministic order by peer ID
	slices.SortFunc(out, func(a, b peer.AddrInfo) int { return strings.Compare(a.ID.String(), b.ID.String()) })
	return out
}

// PinPeer merges addresses, persists and protects; initiates a connect/backoff if necessary.
func (m *pinnedPeersManager) PinPeer(ai peer.AddrInfo) error {
	if !m.isHostAttached() {
		panic("PinPeer must be called after AttachHost, use seedPinsFromConfig instead.")
	}
	if ai.ID == "" {
		return fmt.Errorf("empty peer id")
	}
	if len(ai.Addrs) == 0 {
		return fmt.Errorf("pinned peer requires full multiaddr with address")
	}
	merged := m.mergePinned(ai)
	m.addToPeerStoreAndProtect(merged)
	go m.keepalive.enqueue(merged, true)
	return nil
}

// UnpinPeer removes a pinned peer and cancels protection/backoff but does not disconnect existing connections.
func (m *pinnedPeersManager) UnpinPeer(id peer.ID) error {
	if !m.isHostAttached() {
		panic("pinnedPeersManager used before AttachHost: UnpinPeer")
	}
	if id == "" {
		return fmt.Errorf("empty peer id")
	}
	if !m.peers.Delete(id) {
		return fmt.Errorf("peer not pinned")
	}
	m.connMgr.Unprotect(id, pinnedPeerTag)
	m.host.Peerstore().ClearAddrs(id)
	m.host.Peerstore().RemovePeer(id)
	m.keepalive.cleanupPeer(id)
	return nil
}

// onDiscovered merges addresses of a pinned peer and refreshes peerstore/protection.
func (m *pinnedPeersManager) onDiscovered(ai peer.AddrInfo) {
	if !m.isPinned(ai.ID) || !m.isHostAttached() {
		return
	}
	if len(ai.Addrs) > 0 {
		merged := m.mergePinned(ai)
		m.addToPeerStoreAndProtect(merged)
		m.logger.Info("updated pinned peer addrs via discovery", fields.PeerID(ai.ID), zap.Int("new_addrs", len(ai.Addrs)))
	}
	m.connMgr.Protect(ai.ID, pinnedPeerTag)
}

// keepalive drives the per‑peer liveness policy for pinned peers.
//
// Lifecycle per peer (high‑level state machine)
//   - Disconnected → Enqueue:
//     enqueue(ai, immediate) puts the peer into the retry loop if not currently
//     connected. When immediate is true, it first performs a single immediate
//     Connect attempt (bounded by pinnedConnectTimeout) and then, if still
//     disconnected, hands control to the backoff scheduler. When immediate is
//     false, it only schedules the backoff loop.
//   - Retry loop (exponential backoff):
//     scheduleConnect(id) uses a keyed Scheduler to run Connect attempts with
//     exponential delays from defaultInitialBackoff up to defaultMaxBackoff.
//     Each attempt calls connect(ctx, AddrInfo{ID: id}) with a timeout of
//     pinnedConnectTimeout; if isConnected(id) remains false, the Scheduler
//     re‑schedules the next attempt.
//   - Connected → Stabilization:
//     onConnected(id) pauses the retry loop (redial.Stop) and starts a
//     stabilization timer (pinnedStabilizeWindow). If the connection survives
//     the window, the timer callback cancels the retry loop (redial.Cancel),
//     resetting backoff state. If the peer disconnects before the window fires,
//     onDisconnected resumes the paused loop (attempt counter preserved).
//   - Disconnected while stabilizing:
//     onDisconnected(id) stops the stabilization timer (if present) and resumes
//     the retry loop via scheduleConnect(id).
//   - Unpin / teardown:
//     cleanupPeer(id) cancels the retry loop and clears any stabilization timer
//     for that peer; no further attempts are made.
//   - Close:
//     close() prevents new schedules, cancels all per‑peer timers and lets any
//     in‑flight callbacks observe closure.
//
// keepalive is instantiated by pinnedPeersManager.AttachHost and relies on two
// host‑bound callbacks: connect and isConnected. Methods are safe for concurrent
// use and can be invoked from network notifications and API paths.
type keepalive struct {
	ctx         context.Context
	logger      *zap.Logger
	redial      *retry.Scheduler
	stabilizers *hashmap.Map[peer.ID, *time.Timer]
	isPinned    func(peer.ID) bool
	connect     func(context.Context, peer.AddrInfo) error
	isConnected func(peer.ID) bool
}

func newKeepalive(ctx context.Context, logger *zap.Logger, isPinned func(peer.ID) bool, connect func(context.Context, peer.AddrInfo) error, isConnected func(peer.ID) bool) *keepalive {
	cfg := retry.BackoffConfig{Initial: defaultInitialBackoff, Max: defaultMaxBackoff}
	return &keepalive{
		ctx:         ctx,
		logger:      logger,
		redial:      retry.NewScheduler(cfg),
		stabilizers: hashmap.New[peer.ID, *time.Timer](),
		isPinned:    isPinned,
		connect:     connect,
		isConnected: isConnected,
	}
}

func (k *keepalive) close() {
	k.redial.Close()
	k.stabilizers.Range(func(_ peer.ID, t *time.Timer) bool {
		t.Stop()
		return true
	})
}

func (k *keepalive) cleanupPeer(id peer.ID) {
	k.redial.Cancel(id.String())
	if t, ok := k.stabilizers.Get(id); ok {
		t.Stop()
		k.stabilizers.Delete(id)
	}
}

func (k *keepalive) scheduleConnect(id peer.ID) {
	k.redial.Schedule(id.String(), func(attempt int) bool {
		k.logger.Debug("redialing pinned peer", fields.PeerID(id), zap.Int("attempt", attempt))
		ctx, cancel := context.WithTimeout(k.ctx, pinnedConnectTimeout)
		defer cancel()
		if err := k.connect(ctx, peer.AddrInfo{ID: id}); err != nil {
			k.logger.Debug("pinned peer connect failed", fields.PeerID(id), zap.Error(err))
		}
		return k.isConnected(id)
	})
}

// scheduleIfDisconnected schedules a backoff connect if the peer is not connected.
// enqueue optionally attempts a single immediate connect, then schedules the
// backoff loop if the peer is still disconnected.
func (k *keepalive) enqueue(ai peer.AddrInfo, immediate bool) {
	if immediate {
		ctx, cancel := context.WithTimeout(k.ctx, pinnedConnectTimeout)
		if err := k.connect(ctx, ai); err != nil {
			k.logger.Error("pinned peer connect attempt", fields.PeerID(ai.ID), zap.Error(err))
		}
		cancel()
	}
	if !k.isConnected(ai.ID) {
		k.scheduleConnect(ai.ID)
	}
}

func (k *keepalive) onConnected(pid peer.ID) {
	if !k.isPinned(pid) {
		return
	}
	k.redial.Stop(pid.String())
	if t, ok := k.stabilizers.Get(pid); ok {
		t.Stop()
		k.stabilizers.Delete(pid)
	}
	k.logger.Info("pinned peer connected; starting stabilization window", fields.PeerID(pid))
	t := time.AfterFunc(pinnedStabilizeWindow, func() {
		if k.isConnected(pid) {
			k.redial.Cancel(pid.String())
		}
		k.stabilizers.Delete(pid)
	})
	k.stabilizers.Set(pid, t)
}

func (k *keepalive) onDisconnected(pid peer.ID) {
	if !k.isPinned(pid) {
		return
	}
	if t, ok := k.stabilizers.Get(pid); ok {
		t.Stop()
		k.stabilizers.Delete(pid)
	}
	k.logger.Info("pinned peer disconnected; scheduling redial", fields.PeerID(pid))
	k.scheduleConnect(pid)
}
