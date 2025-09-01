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
	"go.uber.org/zap/zaptest"

	"github.com/ssvlabs/ssv/utils/hashmap"
	"github.com/ssvlabs/ssv/utils/retry"
)

func genPeerID(t *testing.T) peer.ID {
	t.Helper()
	sk, _, err := libp2pcrypto.GenerateEd25519Key(crand.Reader)
	require.NoError(t, err)
	id, err := peer.IDFromPrivateKey(sk)
	require.NoError(t, err)
	return id
}

func maFor(id peer.ID) string {
	return "/ip4/127.0.0.1/tcp/13001/p2p/" + id.String()
}

func Test_parsePeerList_ValidAndInvalid(t *testing.T) {
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

	logger := zaptest.NewLogger(t)
	grouped := parsePeerList(entries, logger, "pinned")

	// Expect only id1 and id2, with at least one addr each; id1 should have 2 addrs
	require.Len(t, grouped, 2)
	require.Contains(t, grouped, id1)
	require.Contains(t, grouped, id2)
	require.GreaterOrEqual(t, len(grouped[id1]), 2)
	require.GreaterOrEqual(t, len(grouped[id2]), 1)
}

func Test_parsePinnedPeers_AcceptsOnlyMultiaddrs(t *testing.T) {
	id1 := genPeerID(t)
	id2 := genPeerID(t)
	id3 := genPeerID(t)

	cfg := &Config{PinnedPeers: []string{
		maFor(id1), // ok
		maFor(id2) + ",/ip4/127.0.0.1/tcp/13005/p2p/" + id2.String(), // ok (two addrs)
		id3.String(),               // bare id -> ignored
		"/ip4/127.0.0.1/tcp/13001", // invalid -> ignored
	}}

	n := &p2pNetwork{
		cfg:         cfg,
		pinnedPeers: hashmap.New[peer.ID, *peer.AddrInfo](),
		logger:      zaptest.NewLogger(t),
	}

	require.NoError(t, n.parsePinnedPeers())

	// id1 and id2 should be pinned; id3 should be absent
	ai1, ok1 := n.pinnedPeers.Get(id1)
	ai2, ok2 := n.pinnedPeers.Get(id2)
	_, ok3 := n.pinnedPeers.Get(id3)

	require.True(t, ok1)
	require.True(t, ok2)
	require.False(t, ok3)

	require.Equal(t, id1, ai1.ID)
	require.GreaterOrEqual(t, len(ai1.Addrs), 1)
	require.Equal(t, id2, ai2.ID)
	require.GreaterOrEqual(t, len(ai2.Addrs), 2)
}

// no further helpers required

func Test_PinPeer_RejectsNoAddrs(t *testing.T) {
	n := &p2pNetwork{
		pinnedPeers: hashmap.New[peer.ID, *peer.AddrInfo](),
		logger:      zaptest.NewLogger(t),
	}
	// empty ID
	err := n.PinPeer(peer.AddrInfo{})
	require.Error(t, err)
	// valid ID but no addrs
	id := genPeerID(t)
	err = n.PinPeer(peer.AddrInfo{ID: id})
	require.Error(t, err)
}

func Test_UnpinPeer_NotPinned(t *testing.T) {
	n := &p2pNetwork{
		pinnedPeers:           hashmap.New[peer.ID, *peer.AddrInfo](),
		pinnedRedialScheduler: retry.NewScheduler(retry.BackoffConfig{}),
		logger:                zaptest.NewLogger(t),
	}
	err := n.UnpinPeer(genPeerID(t))
	require.Error(t, err)
}

func Test_ListPinned_Snapshot(t *testing.T) {
	id1 := genPeerID(t)
	id2 := genPeerID(t)
	n := &p2pNetwork{
		pinnedPeers: hashmap.New[peer.ID, *peer.AddrInfo](),
		logger:      zaptest.NewLogger(t),
	}
	n.pinnedPeers.Set(id1, &peer.AddrInfo{ID: id1})
	n.pinnedPeers.Set(id2, &peer.AddrInfo{ID: id2})

	list := n.ListPinned()
	require.Len(t, list, 2)
	// ensure we got copies, not pointers to internal map
	// mutate internal and check previous list unchanged
	n.pinnedPeers.Set(id1, &peer.AddrInfo{ID: id1})
	require.Len(t, list, 2)
}

func Test_PinPeer_Success_WithHostAndCanceledCtx(t *testing.T) {
	// Create a libp2p host for peerstore access; no remote connects expected.
	h, err := libp2p.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	// Use a canceled context so background connect goroutine exits quickly.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	n := &p2pNetwork{
		ctx:                   ctx,
		host:                  h,
		pinnedPeers:           hashmap.New[peer.ID, *peer.AddrInfo](),
		pinnedRedialScheduler: retry.NewScheduler(retry.BackoffConfig{}),
		pinnedStabilizers:     hashmap.New[peer.ID, *time.Timer](),
		logger:                zaptest.NewLogger(t),
	}
	t.Cleanup(func() { n.pinnedRedialScheduler.Close() })

	id := genPeerID(t)
	addr, err := ma.NewMultiaddr(maFor(id))
	require.NoError(t, err)

	err = n.PinPeer(peer.AddrInfo{ID: id, Addrs: []ma.Multiaddr{addr}})
	require.NoError(t, err)

	ai, ok := n.pinnedPeers.Get(id)
	require.True(t, ok)
	require.Equal(t, id, ai.ID)
	require.GreaterOrEqual(t, len(ai.Addrs), 1)
}

func Test_OnPinnedPeerDiscovered_NoHost(t *testing.T) {
	id := genPeerID(t)
	n := &p2pNetwork{
		pinnedPeers: hashmap.New[peer.ID, *peer.AddrInfo](),
		logger:      zaptest.NewLogger(t),
	}
	n.pinnedPeers.Set(id, &peer.AddrInfo{ID: id})

	// With nil host and pinned peer, function should return early without panic and without changes
	n.onPinnedPeerDiscovered(peer.AddrInfo{ID: id})

	ai, ok := n.pinnedPeers.Get(id)
	require.True(t, ok)
	require.Equal(t, id, ai.ID)
}
