package consensustest_test

import (
	mrand "math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// TestMeshGossipConfig_WithDefaults pins the SSV-matched defaults: any
// zero field on a partially-populated config is filled with the
// canonical value from network/topics/params/gossipsub.go. Tests both
// the "all-zero" common case and "partial override" to confirm
// non-zero fields are preserved.
func TestMeshGossipConfig_WithDefaults(t *testing.T) {
	got := ct.MeshGossipConfig{}.WithDefaults()
	require.Equal(t, 700*time.Millisecond, got.HeartbeatInterval, "default HeartbeatInterval matches SSV's 700ms override")
	require.Equal(t, 6, got.HistoryLength, "default HistoryLength matches SSV's gsMcacheLen=6")
	require.Equal(t, 4, got.HistoryGossip, "default HistoryGossip matches SSV's gsMcacheGossip=4")
	require.Equal(t, 6, got.Dlazy, "default Dlazy is the libp2p default (SSV doesn't override)")
	require.Equal(t, 0.25, got.GossipFactor)

	partial := ct.MeshGossipConfig{HeartbeatInterval: 1 * time.Second, Dlazy: 12}.WithDefaults()
	require.Equal(t, 1*time.Second, partial.HeartbeatInterval, "non-zero HeartbeatInterval is preserved")
	require.Equal(t, 12, partial.Dlazy, "non-zero Dlazy is preserved")
	require.Equal(t, 6, partial.HistoryLength, "still-zero HistoryLength defaults")
	require.Equal(t, 0.25, partial.GossipFactor, "still-zero GossipFactor defaults")
}

// TestMeshGossip_MCacheLifecycle exercises the per-node mcache:
// insert is idempotent on msgID, lookup returns inserted entries,
// rotate advances the head, and rotating through HistoryLength slots
// evicts a mid inserted in the original head.
func TestMeshGossip_MCacheLifecycle(t *testing.T) {
	mesh := newTestMesh(t /* n */, 4)
	const history = 3
	node := ct.MeshNode(0)

	var reinjectCalled int
	entry := ct.MCacheEntry{
		Kind:     ct.KindLeaderBroadcast,
		Bytes:    1024,
		Reinject: func(_ ct.MeshNode) { reinjectCalled++ },
	}

	mid := ct.MsgID(42)
	mesh.MCacheInsert(node, mid, entry, history)

	got, ok := mesh.MCacheLookup(node, mid)
	require.True(t, ok, "lookup hits immediately after insert")
	require.Equal(t, ct.KindLeaderBroadcast, got.Kind)
	require.Equal(t, int64(1024), got.Bytes)

	// Idempotence: second insert with the same mid is a no-op (no
	// duplicate slot entry, original entry retained).
	noopEntry := ct.MCacheEntry{Kind: ct.KindCommit}
	mesh.MCacheInsert(node, mid, noopEntry, history)
	got, _ = mesh.MCacheLookup(node, mid)
	require.Equal(t, ct.KindLeaderBroadcast, got.Kind, "second insert is a no-op; original entry retained")

	// One rotation: head moves forward; mid is still in cache (it's in
	// the previous slot, not yet evicted).
	mesh.MCacheRotate(node)
	_, ok = mesh.MCacheLookup(node, mid)
	require.True(t, ok, "mid still in cache after 1 rotation (history=3)")

	// Two more rotations bring us back around to the original slot,
	// which now gets cleared on eviction.
	mesh.MCacheRotate(node)
	mesh.MCacheRotate(node)
	_, ok = mesh.MCacheLookup(node, mid)
	require.False(t, ok, "mid evicted after history rotations")

	require.Zero(t, reinjectCalled, "Reinject is only called on explicit invocation, never by lifecycle ops")
}

// TestMeshGossip_GossipMids_Window confirms MCacheGossipMids returns
// the union of mids across the last `window` slots — not the entire
// HistoryLength cache, and never older than the window.
func TestMeshGossip_GossipMids_Window(t *testing.T) {
	mesh := newTestMesh(t, 4)
	const history = 4
	node := ct.MeshNode(0)

	// Insert mid=1 in slot 0, rotate, insert mid=2 in slot 1, rotate,
	// insert mid=3 in slot 2, rotate, insert mid=4 in slot 3.
	insertAt := func(mid ct.MsgID) {
		mesh.MCacheInsert(node, mid, ct.MCacheEntry{Reinject: func(_ ct.MeshNode) {}}, history)
	}
	insertAt(1)
	mesh.MCacheRotate(node)
	insertAt(2)
	mesh.MCacheRotate(node)
	insertAt(3)
	mesh.MCacheRotate(node)
	insertAt(4)

	// Current head is slot 3 (mid=4). Walking back 2 slots covers
	// slots 3 and 2 → {4, 3}.
	mids := mesh.MCacheGossipMids(node, 2)
	require.ElementsMatch(t, []ct.MsgID{4, 3}, mids, "window=2 returns last two slots")

	mids = mesh.MCacheGossipMids(node, 4)
	require.ElementsMatch(t, []ct.MsgID{4, 3, 2, 1}, mids, "window=HistoryLength returns the full cache")

	// Larger window is clamped to history; same result as full.
	mids = mesh.MCacheGossipMids(node, 99)
	require.ElementsMatch(t, []ct.MsgID{4, 3, 2, 1}, mids, "oversized window clamps to history")
}

// TestMeshGossip_NonMeshPeers verifies the pool used for IHAVE
// recipient selection: every mesh node except `node` itself and its
// direct mesh-neighbors. The exact neighbor degree varies per node at
// small cluster sizes (relays' protocol-edge count is a random draw
// during topology construction), so we only assert the structural
// invariants — not a specific pool size.
func TestMeshGossip_NonMeshPeers(t *testing.T) {
	mesh := newTestMesh(t, 4)
	for i := 0; i < mesh.TotalNodes(); i++ {
		node := ct.MeshNode(i)
		pool := mesh.NonMeshPeers(node)
		nbrs := mesh.Neighbors(node)
		require.Len(t, pool, mesh.TotalNodes()-1-len(nbrs),
			"node %d: pool = total - self - mesh-neighbors", i)
		// Pool excludes self.
		for _, p := range pool {
			require.NotEqual(t, node, p, "pool excludes self")
		}
		// Pool excludes mesh neighbors.
		for _, nbr := range nbrs {
			for _, p := range pool {
				require.NotEqual(t, nbr, p, "pool excludes mesh neighbor %d", nbr)
			}
		}
	}
}

// TestMeshGossip_PickRecipients_DeterministicAndCapped checks that
// PickGossipRecipients respects Dlazy / GossipFactor and that the
// same RNG seed yields the same selection (so the heartbeat path is
// reproducible).
func TestMeshGossip_PickRecipients_DeterministicAndCapped(t *testing.T) {
	mesh := newTestMesh(t, 4)
	node := ct.MeshNode(0)
	pool := mesh.NonMeshPeers(node)
	require.NotEmpty(t, pool)

	// Dlazy > pool: should be capped to pool size.
	a := mesh.PickGossipRecipients(mrand.New(mrand.NewSource(1)), node, 99, 0.25)
	require.Len(t, a, len(pool), "Dlazy=99 caps at pool size")

	// Same seed → same selection.
	b := mesh.PickGossipRecipients(mrand.New(mrand.NewSource(7)), node, 2, 0.25)
	c := mesh.PickGossipRecipients(mrand.New(mrand.NewSource(7)), node, 2, 0.25)
	require.Equal(t, b, c, "same seed reproduces selection")

	// Different seed → likely different order or selection (probabilistic;
	// at pool size 4 there's a small but non-zero chance of identical
	// permutation, so we don't assert inequality — just that the test
	// doesn't crash).
	_ = mesh.PickGossipRecipients(mrand.New(mrand.NewSource(13)), node, 2, 0.25)
}

// newTestMesh builds a minimal MeshTopology for the unit tests above:
// uses a constant-zero hop delay so any sim-related sampling paths
// don't need an RNG seeded by something specific. Not used for any
// behavior test — purely a structural fixture.
func newTestMesh(t *testing.T, n int) *ct.MeshTopology {
	t.Helper()
	cluster := ct.MakeOperators(n)
	return ct.NewMeshTopology(
		/* seed */ 1,
		ct.MeshConfig{
			HopDelay: ct.ConstantDelay{D: 0},
		},
		cluster,
	)
}
