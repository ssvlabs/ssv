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

// pinnedPeersManager encapsulates pinned peers state and lifecycle.
// It implements List/Pin/Unpin and integrates with libp2p host to
// persist addresses, protect peers and redial on disconnect.
type pinnedPeersManager struct {
	ctx    context.Context
	logger *zap.Logger

	host    host.Host
	connMgr connmgrcore.ConnManager

	peers       *hashmap.Map[peer.ID, *peer.AddrInfo]
	redial      *retry.Scheduler
	stabilizers *hashmap.Map[peer.ID, *time.Timer]

	protectTag      string
	stabilizeWindow time.Duration
}

func newPinnedPeersManager(ctx context.Context, logger *zap.Logger, backoff retry.BackoffConfig, stabilizeWindow time.Duration) *pinnedPeersManager {
	if backoff.Initial <= 0 {
		backoff.Initial = 10 * time.Second
	}
	if backoff.Max <= 0 {
		backoff.Max = 10 * time.Minute
	}
	return &pinnedPeersManager{
		ctx:             ctx,
		logger:          logger,
		peers:           hashmap.New[peer.ID, *peer.AddrInfo](),
		redial:          retry.NewScheduler(backoff),
		stabilizers:     hashmap.New[peer.ID, *time.Timer](),
		protectTag:      pinnedPeerTag,
		stabilizeWindow: stabilizeWindow,
	}
}

func (m *pinnedPeersManager) Close() {
	if m.redial != nil {
		m.redial.Close()
	}
	if m.stabilizers != nil {
		m.stabilizers.Range(func(_ peer.ID, t *time.Timer) bool {
			if t != nil {
				t.Stop()
			}
			return true
		})
	}
}

// AttachHost wires the manager to a libp2p host/connmgr; persists addrs, protects peers, and registers redial hooks.
func (m *pinnedPeersManager) AttachHost(h host.Host, cm connmgrcore.ConnManager) {
	if h == nil {
		return
	}
	// Idempotency: if we've already attached a host, do nothing.
	if m.host != nil {
		return
	}
	m.host = h
	m.connMgr = cm
	// Persist addrs & protect; kick off redial if not yet connected.
	m.peers.Range(func(_ peer.ID, ai *peer.AddrInfo) bool {
		if ai == nil {
			return true
		}
		if len(ai.Addrs) > 0 {
			m.host.Peerstore().AddAddrs(ai.ID, ai.Addrs, peerstore.PermanentAddrTTL)
		}
		if m.connMgr != nil {
			m.connMgr.Protect(ai.ID, m.protectTag)
		}
		if m.host.Network().Connectedness(ai.ID) != p2pnet.Connected {
			m.scheduleConnect(ai.ID)
		}
		return true
	})

	// Redial on disconnect; stop attempts on connect and start stabilization window.
	m.host.Network().Notify(&p2pnet.NotifyBundle{
		ConnectedF:    func(_ p2pnet.Network, c p2pnet.Conn) { m.onConnected(c.RemotePeer()) },
		DisconnectedF: func(_ p2pnet.Network, c p2pnet.Conn) { m.onDisconnected(c.RemotePeer()) },
	})
}

func (m *pinnedPeersManager) IsPinned(id peer.ID) bool { return m.peers.Has(id) }

// SeedPins initializes the manager with a list of pinned peers. It should be
// called during setup, before AttachHost. It defensively rejects being called
// after the host is attached to avoid surprising late mutations of protection
// and redial hooks.
func (m *pinnedPeersManager) SeedPins(pins []peer.AddrInfo) error {
	if m.host != nil {
		return fmt.Errorf("SeedPins must be called before AttachHost")
	}
	for _, ai := range pins {
		if ai.ID == "" || len(ai.Addrs) == 0 {
			continue
		}
		m.peers.Set(ai.ID, &peer.AddrInfo{ID: ai.ID, Addrs: p2pcommons.DedupMultiaddrs(ai.Addrs)})
	}
	return nil
}

// FilterUnpinned returns a new slice containing only peers that are not pinned.
func (m *pinnedPeersManager) FilterUnpinned(ids []peer.ID) []peer.ID {
	if len(ids) == 0 {
		return ids
	}
	out := make([]peer.ID, 0, len(ids))
	for _, id := range ids {
		if !m.IsPinned(id) {
			out = append(out, id)
		}
	}
	return out
}

// ListPinned returns a copy snapshot.
func (m *pinnedPeersManager) ListPinned() []peer.AddrInfo {
	out := make([]peer.AddrInfo, 0)
	m.peers.Range(func(_ peer.ID, ai *peer.AddrInfo) bool {
		if ai != nil {
			cp := peer.AddrInfo{ID: ai.ID, Addrs: append([]ma.Multiaddr(nil), ai.Addrs...)}
			out = append(out, cp)
		}
		return true
	})
	// deterministic order by peer ID
	slices.SortFunc(out, func(a, b peer.AddrInfo) int { return strings.Compare(a.ID.String(), b.ID.String()) })
	return out
}

// PinPeer merges addresses, persists and protects; initiates a connect/backoff if necessary.
func (m *pinnedPeersManager) PinPeer(ai peer.AddrInfo) error {
	if ai.ID == "" {
		return fmt.Errorf("empty peer id")
	}
	if len(ai.Addrs) == 0 {
		return fmt.Errorf("pinned peer requires full multiaddr with address")
	}
	if m.host == nil {
		return fmt.Errorf("p2p host is not initialized")
	}
	if existing, ok := m.peers.Get(ai.ID); ok && existing != nil {
		existing = &peer.AddrInfo{ID: existing.ID, Addrs: p2pcommons.DedupMultiaddrs(append(existing.Addrs, ai.Addrs...))}
		m.peers.Set(ai.ID, existing)
	} else {
		m.peers.Set(ai.ID, &peer.AddrInfo{ID: ai.ID, Addrs: p2pcommons.DedupMultiaddrs(append([]ma.Multiaddr(nil), ai.Addrs...))})
	}
	m.host.Peerstore().AddAddrs(ai.ID, ai.Addrs, peerstore.PermanentAddrTTL)
	if m.connMgr != nil {
		m.connMgr.Protect(ai.ID, m.protectTag)
	}
	go func(ai peer.AddrInfo) {
		ctx, cancel := context.WithTimeout(m.ctx, 20*time.Second)
		defer cancel()
		_ = m.host.Connect(ctx, ai)
		if m.host.Network().Connectedness(ai.ID) != p2pnet.Connected {
			m.scheduleConnect(ai.ID)
		}
	}(ai)
	return nil
}

// UnpinPeer removes a pinned peer and cancels protection/backoff.
func (m *pinnedPeersManager) UnpinPeer(id peer.ID) error {
	if id == "" {
		return fmt.Errorf("empty peer id")
	}
	if !m.peers.Delete(id) {
		return fmt.Errorf("peer not pinned")
	}
	if m.connMgr != nil {
		m.connMgr.Unprotect(id, m.protectTag)
	}
	m.redial.Cancel(id.String())
	if t, ok := m.stabilizers.Get(id); ok && t != nil {
		t.Stop()
		m.stabilizers.Delete(id)
	}
	return nil
}

// OnDiscovered merges addresses of a pinned peer and refreshes peerstore/protection.
func (m *pinnedPeersManager) OnDiscovered(ai peer.AddrInfo) {
	if !m.IsPinned(ai.ID) || m.host == nil {
		return
	}
	if len(ai.Addrs) > 0 {
		if existing, ok := m.peers.Get(ai.ID); ok && existing != nil {
			existing = &peer.AddrInfo{ID: existing.ID, Addrs: p2pcommons.DedupMultiaddrs(append(existing.Addrs, ai.Addrs...))}
			m.peers.Set(ai.ID, existing)
		} else {
			m.peers.Set(ai.ID, &peer.AddrInfo{ID: ai.ID, Addrs: p2pcommons.DedupMultiaddrs(ai.Addrs)})
		}
		m.host.Peerstore().AddAddrs(ai.ID, ai.Addrs, peerstore.PermanentAddrTTL)
		m.logger.Info("updated pinned peer addrs via discovery", fields.PeerID(ai.ID), zap.Int("new_addrs", len(ai.Addrs)))
	}
	if m.connMgr != nil {
		m.connMgr.Protect(ai.ID, m.protectTag)
	}
}

func (m *pinnedPeersManager) onConnected(pid peer.ID) {
	if !m.IsPinned(pid) {
		return
	}
	m.redial.Stop(pid.String())
	if t, ok := m.stabilizers.Get(pid); ok && t != nil {
		t.Stop()
		m.stabilizers.Delete(pid)
	}
	t := time.AfterFunc(m.stabilizeWindow, func() {
		if m.host != nil && m.host.Network().Connectedness(pid) == p2pnet.Connected {
			m.redial.Cancel(pid.String())
		}
		m.stabilizers.Delete(pid)
	})
	m.stabilizers.Set(pid, t)
	m.logger.Info("pinned peer connected; starting stabilization window", fields.PeerID(pid))
}

func (m *pinnedPeersManager) onDisconnected(pid peer.ID) {
	ai, ok := m.peers.Get(pid)
	if !ok {
		return
	}
	if t, ok := m.stabilizers.Get(pid); ok && t != nil {
		t.Stop()
		m.stabilizers.Delete(pid)
	}
	m.logger.Info("pinned peer disconnected; scheduling redial", fields.PeerID(pid))
	m.scheduleConnect(ai.ID)
}

func (m *pinnedPeersManager) scheduleConnect(id peer.ID) {
	m.redial.Schedule(id.String(), func(attempt int) bool {
		m.logger.Info("redialing pinned peer", fields.PeerID(id), zap.Int("attempt", attempt))
		ctx2, cancel2 := context.WithTimeout(m.ctx, 20*time.Second)
		defer cancel2()
		if err := m.host.Connect(ctx2, peer.AddrInfo{ID: id}); err != nil {
			m.logger.Debug("pinned peer connect failed", fields.PeerID(id), zap.Error(err))
		}
		return m.host.Network().Connectedness(id) == p2pnet.Connected
	})
}
