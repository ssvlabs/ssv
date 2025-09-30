package p2pv1

import (
	"context"
	crand "crypto/rand"
	"testing"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/observability/log"
)

// test helpers shared across pinned manager tests
func genPeerID(t *testing.T) peer.ID {
	t.Helper()
	sk, _, err := libp2pcrypto.GenerateEd25519Key(crand.Reader)
	require.NoError(t, err)
	id, err := peer.IDFromPrivateKey(sk)
	require.NoError(t, err)
	return id
}

func maFor(id peer.ID) string { return "/ip4/127.0.0.1/tcp/13001/p2p/" + id.String() }

func TestPinnedManager_PinPeer_RejectsNoAddrs(t *testing.T) {
	h, err := libp2p.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	m := newPinnedPeersManager(t.Context(), log.TestLogger(t))
	m.attachHost(h, h.ConnManager())
	// empty ID
	err = m.PinPeer(peer.AddrInfo{})
	require.Error(t, err)
	require.ErrorContains(t, err, "empty peer id")
	// valid ID but no addrs
	id := genPeerID(t)
	err = m.PinPeer(peer.AddrInfo{ID: id})
	require.Error(t, err)
	require.ErrorContains(t, err, "requires full multiaddr")
}

func TestPinnedManager_UnpinPeer_NotPinned(t *testing.T) {
	h, err := libp2p.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	m := newPinnedPeersManager(t.Context(), log.TestLogger(t))
	m.attachHost(h, h.ConnManager())
	t.Cleanup(func() { m.close() })
	err = m.UnpinPeer(genPeerID(t))
	require.Error(t, err)
	require.ErrorContains(t, err, "peer not pinned")
}

func TestPinnedManager_ListPinned_SnapshotAndOrder(t *testing.T) {
	id1 := genPeerID(t)
	id2 := genPeerID(t)
	m := newPinnedPeersManager(t.Context(), log.TestLogger(t))
	// seed
	// insert in reverse order to verify sorting is by ID
	m.peers.Set(id2, &peer.AddrInfo{ID: id2})
	m.peers.Set(id1, &peer.AddrInfo{ID: id1})

	list := m.ListPinned()
	require.Len(t, list, 2)
	// Deterministic order: sorted by peer ID string
	exp := []string{id1.String(), id2.String()}
	if exp[0] > exp[1] {
		exp[0], exp[1] = exp[1], exp[0]
	}
	got := []string{list[0].ID.String(), list[1].ID.String()}
	require.Equal(t, exp, got)
}

func TestPinnedManager_PinPeer_Success_WithHostAndCanceledCtx(t *testing.T) {
	// Create a libp2p host for peerstore access; no remote connects expected.
	h, err := libp2p.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	// Use a canceled context so background connect goroutine exits quickly.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	m := newPinnedPeersManager(ctx, log.TestLogger(t))
	m.attachHost(h, h.ConnManager())
	t.Cleanup(func() { m.close() })

	id := genPeerID(t)
	addr, err := ma.NewMultiaddr(maFor(id))
	require.NoError(t, err)

	err = m.PinPeer(peer.AddrInfo{ID: id, Addrs: []ma.Multiaddr{addr}})
	require.NoError(t, err)

	ai, ok := m.peers.Get(id)
	require.True(t, ok)
	require.Equal(t, id, ai.ID)
	require.GreaterOrEqual(t, len(ai.Addrs), 1)
}

func TestPinnedManager_OnPinnedPeerDiscovered_NoHost(t *testing.T) {
	id := genPeerID(t)
	m := newPinnedPeersManager(t.Context(), log.TestLogger(t))
	m.peers.Set(id, &peer.AddrInfo{ID: id})

	// With nil host and pinned peer, function should return early without panic and without changes
	m.onDiscovered(peer.AddrInfo{ID: id})

	ai, ok := m.peers.Get(id)
	require.True(t, ok)
	require.Equal(t, id, ai.ID)
}

func TestPinnedManager_AttachHost_PersistsAddrsAndProtects(t *testing.T) {
	h, err := libp2p.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	id := genPeerID(t)
	// peerstore expects base addrs without /p2p component
	addr, err := ma.NewMultiaddr("/ip4/127.0.0.1/tcp/13001")
	require.NoError(t, err)

	m := newPinnedPeersManager(t.Context(), log.TestLogger(t))
	t.Cleanup(func() { m.close() })

	m.peers.Set(id, &peer.AddrInfo{ID: id, Addrs: []ma.Multiaddr{addr}})
	m.attachHost(h, h.ConnManager())

	addrs := h.Peerstore().Addrs(id)
	found := false
	for _, a := range addrs {
		if a.Equal(addr) {
			found = true
			break
		}
	}
	require.True(t, found, "expected pinned peer address to be persisted")
}

func TestPinnedManager_AttachHost_PanicsOnNilInputs(t *testing.T) {
	m := newPinnedPeersManager(t.Context(), log.TestLogger(t))
	// nil host and nil conn manager
	require.Panics(t, func() { m.attachHost(nil, nil) })

	// non-nil host but nil conn manager
	h, err := libp2p.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })
	require.Panics(t, func() { m.attachHost(h, nil) })
}

func TestPinnedManager_AttachHost_PanicsOnSecondCall(t *testing.T) {
	h, err := libp2p.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	m := newPinnedPeersManager(t.Context(), log.TestLogger(t))
	m.attachHost(h, h.ConnManager())
	require.Panics(t, func() { m.attachHost(h, h.ConnManager()) })
}

func TestPinnedManager_OnConnected_SetsStabilizationTimer(t *testing.T) {
	id := genPeerID(t)
	h, err := libp2p.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	m := newPinnedPeersManager(t.Context(), log.TestLogger(t))
	m.attachHost(h, h.ConnManager())
	m.peers.Set(id, &peer.AddrInfo{ID: id})

	m.keepalive.onConnected(id)
	if _, ok := m.keepalive.stabilizers.Get(id); !ok {
		t.Fatalf("expected stabilization timer set for %s", id)
	}
}

func TestPinnedManager_OnDisconnected_ClearsTimer(t *testing.T) {
	id := genPeerID(t)
	h, err := libp2p.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	m := newPinnedPeersManager(t.Context(), log.TestLogger(t))
	m.attachHost(h, h.ConnManager())
	t.Cleanup(func() { m.close() })
	m.peers.Set(id, &peer.AddrInfo{ID: id})
	tmr := time.AfterFunc(time.Hour, func() {})
	m.keepalive.stabilizers.Set(id, tmr)

	m.keepalive.onDisconnected(id)
	if _, ok := m.keepalive.stabilizers.Get(id); ok {
		t.Fatalf("expected stabilization timer cleared for %s", id)
	}
}

func TestPinnedManager_UnpinPeer_ClearsKeepalive(t *testing.T) {
	id := genPeerID(t)

	h, err := libp2p.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	m := newPinnedPeersManager(t.Context(), log.TestLogger(t))
	m.attachHost(h, h.ConnManager())
	t.Cleanup(func() { m.close() })

	// Mark as pinned to allow UnpinPeer to proceed.
	m.peers.Set(id, &peer.AddrInfo{ID: id})

	// Seed a stabilization timer.
	tmr := time.AfterFunc(time.Hour, func() {})
	m.keepalive.stabilizers.Set(id, tmr)

	// Now unpin: this must clear stabilization timer.
	require.NoError(t, m.UnpinPeer(id))

	if _, ok := m.keepalive.stabilizers.Get(id); ok {
		t.Fatalf("expected stabilization timer cleared for %s", id)
	}

	// No need to rely on timing for redial cancellation here.
}

func TestPinnedManager_SeedPins_AfterAttachHost_Panics(t *testing.T) {
	h, err := libp2p.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	logger := log.TestLogger(t)
	m := newPinnedPeersManager(t.Context(), logger)
	m.attachHost(h, h.ConnManager())

	id := genPeerID(t)
	addr, err := ma.NewMultiaddr(maFor(id))
	require.NoError(t, err)
	pins := []peer.AddrInfo{{ID: id, Addrs: []ma.Multiaddr{addr}}}

	require.Panics(t, func() { m.seedPinsFromConfig(pins) })
}

// parsePeers: all valid entries succeed and deduplicate by ID.
func Test_parsePeers_AllValid(t *testing.T) {
	id1 := genPeerID(t)
	id2 := genPeerID(t)

	good1 := maFor(id1)
	good2 := maFor(id2)

	entries := []string{
		good1 + ",/ip4/127.0.0.1/tcp/13002/p2p/" + id1.String(), // second addr for id1
		good2,
	}

	logger := log.TestLogger(t)
	parsed, err := parsePeers("pinned", entries, logger)
	require.NoError(t, err)
	require.Len(t, parsed, 2)
	mp := map[peer.ID]int{}
	for _, ai := range parsed {
		mp[ai.ID] = len(ai.Addrs)
	}
	require.Contains(t, mp, id1)
	require.Contains(t, mp, id2)
	require.Equal(t, 2, mp[id1])
	require.Equal(t, 1, mp[id2])
}

// parsePeers: any invalid entry returns an error.
func Test_parsePeers_InvalidReturnsError(t *testing.T) {
	id1 := genPeerID(t)
	id2 := genPeerID(t)

	good1 := maFor(id1)
	good2 := maFor(id2)

	entries := []string{
		good1,
		good2,
		id2.String(),               // bare ID -> invalid
		"/ip4/127.0.0.1/tcp/13003", // missing /p2p -> invalid
	}

	logger := log.TestLogger(t)
	_, err := parsePeers("pinned", entries, logger)
	require.Error(t, err)
}

func Test_parsePinnedPeers_ValidOnly(t *testing.T) {
	id1 := genPeerID(t)
	id2 := genPeerID(t)

	entries := []string{
		maFor(id1), // ok
		maFor(id2) + ",/ip4/127.0.0.1/tcp/13005/p2p/" + id2.String(), // ok (two addrs)
	}

	logger := log.TestLogger(t)
	m := newPinnedPeersManager(t.Context(), logger)
	pins, err := parsePeers("pinned", entries, logger)
	require.NoError(t, err)
	m.seedPinsFromConfig(pins)

	// id1 and id2 should be pinned
	list := m.ListPinned()
	require.Len(t, list, 2)
	// verify the two IDs exist and counts
	found1 := false
	found2 := false
	for _, ai := range list {
		if ai.ID == id1 {
			found1 = true
			require.GreaterOrEqual(t, len(ai.Addrs), 1)
		}
		if ai.ID == id2 {
			found2 = true
			require.GreaterOrEqual(t, len(ai.Addrs), 2)
		}
	}
	require.True(t, found1)
	require.True(t, found2)
}
