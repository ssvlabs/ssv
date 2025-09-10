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
	"github.com/ssvlabs/ssv/utils/retry"
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
	m := newPinnedPeersManager(t.Context(), log.TestLogger(t), retry.BackoffConfig{}, 30*time.Second)
	// empty ID
	err := m.PinPeer(peer.AddrInfo{})
	require.Error(t, err)
	require.ErrorContains(t, err, "empty peer id")
	// valid ID but no addrs
	id := genPeerID(t)
	err = m.PinPeer(peer.AddrInfo{ID: id})
	require.Error(t, err)
	require.ErrorContains(t, err, "requires full multiaddr")
}

func TestPinnedManager_UnpinPeer_NotPinned(t *testing.T) {
	m := newPinnedPeersManager(t.Context(), log.TestLogger(t), retry.BackoffConfig{}, 30*time.Second)
	t.Cleanup(func() { m.Close() })
	err := m.UnpinPeer(genPeerID(t))
	require.Error(t, err)
	require.ErrorContains(t, err, "peer not pinned")
}

func TestPinnedManager_ListPinned_SnapshotAndOrder(t *testing.T) {
	id1 := genPeerID(t)
	id2 := genPeerID(t)
	m := newPinnedPeersManager(t.Context(), log.TestLogger(t), retry.BackoffConfig{}, 30*time.Second)
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

	m := newPinnedPeersManager(ctx, log.TestLogger(t), retry.BackoffConfig{}, 30*time.Second)
	m.AttachHost(h, h.ConnManager())
	t.Cleanup(func() { m.Close() })

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
	m := newPinnedPeersManager(t.Context(), log.TestLogger(t), retry.BackoffConfig{}, 30*time.Second)
	m.peers.Set(id, &peer.AddrInfo{ID: id})

	// With nil host and pinned peer, function should return early without panic and without changes
	m.OnDiscovered(peer.AddrInfo{ID: id})

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

	m := newPinnedPeersManager(t.Context(), log.TestLogger(t), retry.BackoffConfig{Initial: 5 * time.Millisecond, Max: 5 * time.Millisecond}, 30*time.Second)
	t.Cleanup(func() { m.Close() })

	m.peers.Set(id, &peer.AddrInfo{ID: id, Addrs: []ma.Multiaddr{addr}})
	m.AttachHost(h, h.ConnManager())

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

func TestPinnedManager_OnConnected_SetsStabilizationTimer(t *testing.T) {
	id := genPeerID(t)
	m := newPinnedPeersManager(t.Context(), log.TestLogger(t), retry.BackoffConfig{}, 30*time.Second)
	m.peers.Set(id, &peer.AddrInfo{ID: id})

	m.onConnected(id)
	if _, ok := m.stabilizers.Get(id); !ok {
		t.Fatalf("expected stabilization timer set for %s", id)
	}
}

func TestPinnedManager_OnDisconnected_ClearsTimer(t *testing.T) {
	id := genPeerID(t)
	m := newPinnedPeersManager(t.Context(), log.TestLogger(t), retry.BackoffConfig{}, 30*time.Second)
	t.Cleanup(func() { m.Close() })
	m.peers.Set(id, &peer.AddrInfo{ID: id})
	tmr := time.AfterFunc(time.Hour, func() {})
	m.stabilizers.Set(id, tmr)

	m.onDisconnected(id)
	if _, ok := m.stabilizers.Get(id); ok {
		t.Fatalf("expected stabilization timer cleared for %s", id)
	}
}

func TestPinnedManager_SeedPins_AfterAttachHost_Errors(t *testing.T) {
	h, err := libp2p.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	logger := log.TestLogger(t)
	m := newPinnedPeersManager(t.Context(), logger, retry.BackoffConfig{}, 30*time.Second)
	m.AttachHost(h, h.ConnManager())

	id := genPeerID(t)
	addr, err := ma.NewMultiaddr(maFor(id))
	require.NoError(t, err)
	pins := []peer.AddrInfo{{ID: id, Addrs: []ma.Multiaddr{addr}}}

	err = m.SeedPins(pins)
	require.Error(t, err)
	require.ErrorContains(t, err, "SeedPins must be called before AttachHost")
}

// parse and p2pNetwork pinned config integration
func Test_parsePeers_ValidAndInvalid(t *testing.T) {
	id1 := genPeerID(t)
	id2 := genPeerID(t)

	good1 := maFor(id1)
	good2 := maFor(id2)

	entries := []string{
		good1 + ",  /ip4/127.0.0.1/tcp/13002/p2p/" + id1.String(), // second addr for id1
		good2,
		id2.String(),               // bare ID should be ignored
		"/ip4/127.0.0.1/tcp/13003", // missing /p2p -> invalid
		"",
		"   ",
	}

	logger := log.TestLogger(t)
	parsed := parsePeers("pinned", entries, logger)
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

func Test_parsePinnedPeers_AcceptsOnlyMultiaddrs(t *testing.T) {
	id1 := genPeerID(t)
	id2 := genPeerID(t)
	id3 := genPeerID(t)

	entries := []string{
		maFor(id1), // ok
		maFor(id2) + ",/ip4/127.0.0.1/tcp/13005/p2p/" + id2.String(), // ok (two addrs)
		id3.String(),               // bare id -> ignored
		"/ip4/127.0.0.1/tcp/13001", // invalid -> ignored
	}

	logger := log.TestLogger(t)
	m := newPinnedPeersManager(t.Context(), logger, retry.BackoffConfig{}, 30*time.Second)
	pins := parsePeers("pinned", entries, logger)
	require.NoError(t, m.SeedPins(pins))

	// id1 and id2 should be pinned; id3 should be absent
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
