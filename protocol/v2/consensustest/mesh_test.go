package consensustest_test

import (
	"fmt"
	mrand "math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// hopDelay used by the topology builder tests — fixed-value model so the
// builder runs without needing a calibrated LogNormal. Real adapter use
// supplies a LogNormalDelay; here we just need a non-nil NetworkModel.
func testHopDelay() ct.NetworkModel {
	return ct.ConstantDelay{D: 30 * time.Millisecond}
}

// TestMesh_BuildConnected_N4 — the n=4 wiring rule yields 4 protocol +
// 4 relay nodes, every node at degree 3 (1 cluster peer + 2 relays for
// protocol nodes; variable mix for relays summing to ~3). Builder must
// produce a connected graph.
func TestMesh_BuildConnected_N4(t *testing.T) {
	cluster := []ct.OperatorID{1, 2, 3, 4}
	m := ct.NewMeshTopology(42, ct.MeshConfig{HopDelay: testHopDelay()}, cluster)
	require.NotNil(t, m)

	// All four cluster ops are protocol peers.
	for _, op := range cluster {
		node := m.NodeForOperator(op)
		require.True(t, m.IsProtocol(node), "cluster op %d must be a protocol peer", op)
	}

	// Every protocol peer should have exactly 3 neighbors (1 in-cluster
	// + 2 relays per the wiring rule).
	for _, op := range cluster {
		node := m.NodeForOperator(op)
		neighbors := m.Neighbors(node)
		require.Equal(t, 3, len(neighbors),
			"protocol peer op=%d expected degree 3, got %d", op, len(neighbors))
		// Count protocol vs relay neighbors.
		protoCount, relayCount := 0, 0
		for _, nb := range neighbors {
			if m.IsProtocol(nb) {
				protoCount++
			} else {
				relayCount++
			}
		}
		require.Equal(t, 1, protoCount, "protocol peer op=%d expected 1 protocol neighbor", op)
		require.Equal(t, 2, relayCount, "protocol peer op=%d expected 2 relay neighbors", op)
	}
}

// TestMesh_BuildConnected_AllSSVClusterSizes — wiring works across n=4,
// 7, 10, 13 (the SSV-supported sizes). The constructor panics on
// disconnection, so this test passing means every size produces a
// connected graph. n=7 stresses the odd-n branch: the unpaired
// protocol peer gets 0 cluster neighbors + 3 relays, modeling the
// "no co-cluster op in my mesh today" case.
func TestMesh_BuildConnected_AllSSVClusterSizes(t *testing.T) {
	for _, n := range ct.ClusterSizes {
		t.Run(clusterName(n), func(t *testing.T) {
			cluster := ct.MakeOperators(n)
			m := ct.NewMeshTopology(int64(n)*7+1, ct.MeshConfig{HopDelay: testHopDelay()}, cluster)
			require.NotNil(t, m)
			// Protocol peers must all have exactly 3 mesh neighbors with
			// the documented split (0 or 1 cluster neighbor, the rest
			// relays; odd-n leftover gets 0 cluster + 3 relay).
			for _, op := range cluster {
				node := m.NodeForOperator(op)
				neighbors := m.Neighbors(node)
				require.Equal(t, 3, len(neighbors),
					"n=%d op=%d: expected degree 3, got %d", n, op, len(neighbors))
				protoCount, relayCount := 0, 0
				for _, nb := range neighbors {
					if m.IsProtocol(nb) {
						protoCount++
					} else {
						relayCount++
					}
				}
				require.LessOrEqual(t, protoCount, 1,
					"n=%d op=%d: at most 1 cluster neighbor", n, op)
				require.Equal(t, 3-protoCount, relayCount,
					"n=%d op=%d: relay count must fill remaining degree slots", n, op)
			}
			// Relay peers (indices [n, 2n)) must have degree ≥ 3. The
			// topup loop in NewMeshTopology grows each relay until it
			// hits 3 by adding relay-relay edges, but a relay can absorb
			// additional edges when it's chosen as the "other" endpoint
			// by a still-under-degree relay — so the invariant is `≥ 3`,
			// not `= 3`. Without this assertion, a regression that
			// stops the topup early would only be caught indirectly by
			// the framework's "disconnected mesh" panic.
			for i := n; i < m.TotalNodes(); i++ {
				node := ct.MeshNode(i)
				require.Falsef(t, m.IsProtocol(node),
					"n=%d: index %d expected to be a relay (total=%d)", n, i, m.TotalNodes())
				nbrs := m.Neighbors(node)
				require.GreaterOrEqualf(t, len(nbrs), 3,
					"n=%d relay node %d: expected degree ≥ 3, got %d", n, i, len(nbrs))
			}
		})
	}
}

func clusterName(n int) string { return fmt.Sprintf("n=%d", n) }

// TestMesh_BuildDeterministic — same seed → identical wiring. The
// framework's per-sim determinism contract (cfg + seed → byte-identical
// trace) requires the mesh topology to be deterministic-from-seed.
func TestMesh_BuildDeterministic(t *testing.T) {
	cluster := []ct.OperatorID{1, 2, 3, 4}
	cfg := ct.MeshConfig{HopDelay: testHopDelay()}
	m1 := ct.NewMeshTopology(1234, cfg, cluster)
	m2 := ct.NewMeshTopology(1234, cfg, cluster)
	for _, op := range cluster {
		n1 := m1.Neighbors(m1.NodeForOperator(op))
		n2 := m2.Neighbors(m2.NodeForOperator(op))
		require.Equal(t, n1, n2, "deterministic wiring: op %d neighbors differ across builds at same seed", op)
	}
}

// TestMesh_BuildDifferentSeed — different seeds should produce
// different wirings (probabilistically). Asserts the mesh's seed
// actually drives randomness; a tautology-like assertion (same seed
// across two runs is identical) wouldn't catch a wiring algorithm that
// ignored the seed.
func TestMesh_BuildDifferentSeed(t *testing.T) {
	cluster := []ct.OperatorID{1, 2, 3, 4}
	cfg := ct.MeshConfig{HopDelay: testHopDelay()}
	differs := false
	base := ct.NewMeshTopology(1, cfg, cluster)
	for seed := int64(2); seed <= 20; seed++ {
		m := ct.NewMeshTopology(seed, cfg, cluster)
		for _, op := range cluster {
			b := base.Neighbors(base.NodeForOperator(op))
			n := m.Neighbors(m.NodeForOperator(op))
			if len(b) == len(n) {
				for i := range b {
					if b[i] != n[i] {
						differs = true
						break
					}
				}
			} else {
				differs = true
			}
			if differs {
				break
			}
		}
		if differs {
			break
		}
	}
	require.True(t, differs, "20 different seeds all produced identical mesh wiring — seed has no effect")
}

// TestMesh_MarkSeenDedup — MarkSeen returns true on first call, false on
// every subsequent call. The dedup contract is what makes mesh re-flood
// terminate (without it, every neighbor's reflood would loop forever).
func TestMesh_MarkSeenDedup(t *testing.T) {
	cluster := []ct.OperatorID{1, 2, 3, 4}
	m := ct.NewMeshTopology(1, ct.MeshConfig{HopDelay: testHopDelay()}, cluster)
	node := m.NodeForOperator(1)
	id := m.NewMsgID()
	require.True(t, m.MarkSeen(node, id), "first MarkSeen must be true")
	require.False(t, m.MarkSeen(node, id), "second MarkSeen must be false")
	// Different MsgID at the same node still returns true.
	id2 := m.NewMsgID()
	require.True(t, m.MarkSeen(node, id2), "fresh MsgID at same node must be true")
	// Same MsgID at a different node still returns true (per-node dedup).
	otherNode := m.NodeForOperator(2)
	require.True(t, m.MarkSeen(otherNode, id), "same MsgID at fresh node must be true")
}

// TestMesh_NewMsgID_Monotonic — IDs are issued monotonically per topology.
// Sequential allocator; not strict-by-spec but the implementation choice
// today is a simple counter, and the test pins it so an accidental rewrite
// to a randomized allocator would be caught.
func TestMesh_NewMsgID_Monotonic(t *testing.T) {
	m := ct.NewMeshTopology(1, ct.MeshConfig{HopDelay: testHopDelay()}, []ct.OperatorID{1, 2, 3, 4})
	prev := ct.MsgID(0)
	for i := 0; i < 5; i++ {
		id := m.NewMsgID()
		require.Greater(t, id, prev, "MsgID counter must increase")
		prev = id
	}
}

// TestMesh_SampleHopDelay — returns the underlying NetworkModel's delay.
// We use ConstantDelay so the returned value is deterministic regardless
// of rng.
func TestMesh_SampleHopDelay(t *testing.T) {
	const d = 42 * time.Millisecond
	m := ct.NewMeshTopology(1, ct.MeshConfig{HopDelay: ct.ConstantDelay{D: d}}, []ct.OperatorID{1, 2, 3, 4})
	got := m.SampleHopDelay(mrand.New(mrand.NewSource(7)), 1, 2, ct.KindCommit)
	require.Equal(t, d, got)
}

// TestMesh_OperatorAccessors — round-trip OperatorID ↔ MeshNode, and
// EndpointFor coverage for both cluster and relay nodes.
func TestMesh_OperatorAccessors(t *testing.T) {
	cluster := []ct.OperatorID{1, 2, 3, 4}
	m := ct.NewMeshTopology(1, ct.MeshConfig{HopDelay: testHopDelay()}, cluster)
	for _, op := range cluster {
		node := m.NodeForOperator(op)
		require.True(t, m.IsProtocol(node))
		require.Equal(t, op, m.OperatorForNode(node))
		// Cluster endpoints equal the OperatorID.
		require.Equal(t, op, m.EndpointFor(node))
	}
	// Relay nodes resolve to synthetic IDs ≥ RelayEndpointBase, distinct
	// per relay so stateful NetworkModel impls key per-edge.
	seen := make(map[ct.OperatorID]struct{})
	for i := 0; i < len(cluster); i++ {
		relayNode := ct.MeshNode(len(cluster) + i)
		require.False(t, m.IsProtocol(relayNode))
		ep := m.EndpointFor(relayNode)
		require.GreaterOrEqual(t, ep, ct.RelayEndpointBase,
			"relay endpoint must come from the reserved range")
		_, dup := seen[ep]
		require.False(t, dup, "relay endpoint %d duplicated across nodes", ep)
		seen[ep] = struct{}{}
	}
}

// TestMesh_ValidateDelay — passthrough accessor returning the configured
// value. Sanity check; mostly a guard against a future refactor changing
// the field semantics.
func TestMesh_ValidateDelay(t *testing.T) {
	const vd = 7 * time.Millisecond
	m := ct.NewMeshTopology(1, ct.MeshConfig{
		HopDelay:      testHopDelay(),
		ValidateDelay: vd,
	}, []ct.OperatorID{1, 2, 3, 4})
	require.Equal(t, vd, m.ValidateDelay())
}

// TestMesh_GossipPoolBound_RestrictsAtN7 — at n=7 the non-eager pool
// (10 per protocol peer) exceeds the default GossipPoolBound (7), so
// NonMeshPeers returns a strict subset on average. Per-node count is
// a Bernoulli draw with p_g = bound/pool = 0.7 — individual peers can
// occasionally land at the full pool, but the mean across the 7
// protocol peers should sit close to the bound, and the majority of
// peers should see a strict restriction.
func TestMesh_GossipPoolBound_RestrictsAtN7(t *testing.T) {
	cluster := []ct.OperatorID{1, 2, 3, 4, 5, 6, 7}
	m := ct.NewMeshTopology(42, ct.MeshConfig{HopDelay: testHopDelay()}, cluster)
	const pool = 10 // total(14) - 1 - eager(3)
	sumGossip, restricted := 0, 0
	for _, op := range cluster {
		node := m.NodeForOperator(op)
		got := len(m.NonMeshPeers(node))
		require.LessOrEqual(t, got, pool,
			"op=%d: gossip degree %d exceeds non-eager pool %d", op, got, pool)
		if got < pool {
			restricted++
		}
		sumGossip += got
	}
	avg := float64(sumGossip) / float64(len(cluster))
	// Mean across 7 Bernoulli(p=0.7, n=10) draws has stddev ≈ 0.55, so a
	// 2-unit window around the bound is ~3.6σ — robust to seed choice
	// and decisively rejects the unbounded case (mean would be 10).
	require.InDelta(t, 7.0, avg, 2.0,
		"average protocol-peer gossip degree %.2f outside expected 7±2", avg)
	// Per-node P(restricted) = 1 − 0.7^10 ≈ 0.972, so ≥ 4-of-7 is
	// essentially certain even allowing for unlucky seeds.
	require.GreaterOrEqual(t, restricted, 4,
		"expected most protocol peers to be restricted; got %d/%d", restricted, len(cluster))
}

// TestMesh_GossipPoolBound_Symmetric — undirected by construction:
// X ∈ NonMeshPeers(Y) iff Y ∈ NonMeshPeers(X).
func TestMesh_GossipPoolBound_Symmetric(t *testing.T) {
	cluster := []ct.OperatorID{1, 2, 3, 4, 5, 6, 7}
	m := ct.NewMeshTopology(123, ct.MeshConfig{HopDelay: testHopDelay()}, cluster)
	total := ct.MeshNode(m.TotalNodes())
	for a := ct.MeshNode(0); a < total; a++ {
		for _, b := range m.NonMeshPeers(a) {
			found := false
			for _, p := range m.NonMeshPeers(b) {
				if p == a {
					found = true
					break
				}
			}
			require.True(t, found,
				"asymmetry: %d ∈ NonMeshPeers(%d) but %d ∉ NonMeshPeers(%d)",
				b, a, a, b)
		}
	}
}

// TestMesh_GossipPoolBound_Determinism — same seed → identical gossip set.
func TestMesh_GossipPoolBound_Determinism(t *testing.T) {
	cluster := []ct.OperatorID{1, 2, 3, 4, 5, 6, 7}
	cfg := ct.MeshConfig{HopDelay: testHopDelay()}
	m1 := ct.NewMeshTopology(99, cfg, cluster)
	m2 := ct.NewMeshTopology(99, cfg, cluster)
	total := ct.MeshNode(m1.TotalNodes())
	for node := ct.MeshNode(0); node < total; node++ {
		require.Equal(t, m1.NonMeshPeers(node), m2.NonMeshPeers(node),
			"non-mesh peers differ for node %d under identical seed", node)
	}
}

// TestMesh_GossipPoolBound_InactiveAtN4 — at n=4 the non-eager pool
// (4 per protocol peer) is below the default bound (7), so the
// construction is skipped and NonMeshPeers returns the full pool.
// Matches the "small subnet ≈ clique" regime — a real libp2p node in
// such a small subnet would similarly see all peers anyway.
func TestMesh_GossipPoolBound_InactiveAtN4(t *testing.T) {
	cluster := []ct.OperatorID{1, 2, 3, 4}
	m := ct.NewMeshTopology(42, ct.MeshConfig{HopDelay: testHopDelay()}, cluster)
	for _, op := range cluster {
		node := m.NodeForOperator(op)
		require.Equal(t, 4, len(m.NonMeshPeers(node)),
			"op=%d: small-subnet bound should be inactive (expected full pool=4)", op)
	}
}

// TestMesh_GossipPoolBound_ExplicitUnbounded — an explicit bound larger
// than any pool keeps the legacy unbounded NonMeshPeers behaviour (the
// construction-skip path when bound exceeds pool).
func TestMesh_GossipPoolBound_ExplicitUnbounded(t *testing.T) {
	cluster := []ct.OperatorID{1, 2, 3, 4, 5, 6, 7}
	cfg := ct.MeshConfig{
		HopDelay: testHopDelay(),
		Gossip:   ct.MeshGossipConfig{GossipPoolBound: 1 << 30},
	}
	m := ct.NewMeshTopology(42, cfg, cluster)
	for _, op := range cluster {
		node := m.NodeForOperator(op)
		require.Equal(t, 10, len(m.NonMeshPeers(node)),
			"op=%d: explicit huge bound should keep full pool", op)
	}
}

// deliverySetForNode returns the union of `node`'s eager neighbours
// and its gossip candidate set — the full set of mesh nodes it can
// exchange messages with on either layer. Used by the SeverProb
// tests to confirm the access-time filter touches both layers.
func deliverySetForNode(m *ct.MeshTopology, node ct.MeshNode) map[ct.MeshNode]struct{} {
	out := make(map[ct.MeshNode]struct{})
	for _, nb := range m.Neighbors(node) {
		out[nb] = struct{}{}
	}
	for _, peer := range m.NonMeshPeers(node) {
		out[peer] = struct{}{}
	}
	return out
}

// countDeliveryPairs sums |Neighbors| + |NonMeshPeers| across all
// nodes and halves it (each edge counted from both endpoints). Used
// to measure surviving connections under severance.
func countDeliveryPairs(m *ct.MeshTopology) int {
	total := 0
	for a := ct.MeshNode(0); a < ct.MeshNode(m.TotalNodes()); a++ {
		total += len(m.Neighbors(a))
		total += len(m.NonMeshPeers(a))
	}
	return total / 2
}

// TestMesh_SeverProb_RateMatches — across many seeds the surviving
// fraction of delivery pairs should approach 1 − SeverProb. Per-seed
// pairs are compared (same seed: same Layer-1 gossip-connection set,
// so any reduction in the severed build comes from Layer 2 only).
func TestMesh_SeverProb_RateMatches(t *testing.T) {
	cluster := []ct.OperatorID{1, 2, 3, 4, 5, 6, 7}
	const (
		trials    = 100
		severProb = 0.30
	)
	var baseSum, sevSum int
	for seed := int64(1); seed <= trials; seed++ {
		base := ct.NewMeshTopology(seed, ct.MeshConfig{HopDelay: testHopDelay()}, cluster)
		sev := ct.NewMeshTopology(seed, ct.MeshConfig{
			HopDelay:  testHopDelay(),
			SeverProb: severProb,
		}, cluster)
		baseSum += countDeliveryPairs(base)
		sevSum += countDeliveryPairs(sev)
	}
	surviving := float64(sevSum) / float64(baseSum)
	expected := 1.0 - severProb
	require.InDelta(t, expected, surviving, 0.03,
		"surviving fraction %.3f outside expected %.3f ± 0.03 over %d trials",
		surviving, expected, trials)
}

// TestMesh_SeverProb_Symmetric — severance is undirected by
// construction: if X is in Y's delivery set, Y must be in X's.
func TestMesh_SeverProb_Symmetric(t *testing.T) {
	cluster := []ct.OperatorID{1, 2, 3, 4, 5, 6, 7}
	cfg := ct.MeshConfig{HopDelay: testHopDelay(), SeverProb: 0.30}
	m := ct.NewMeshTopology(42, cfg, cluster)
	total := ct.MeshNode(m.TotalNodes())
	for a := ct.MeshNode(0); a < total; a++ {
		setA := deliverySetForNode(m, a)
		for b := ct.MeshNode(0); b < total; b++ {
			if a == b {
				continue
			}
			setB := deliverySetForNode(m, b)
			_, aInB := setB[a]
			_, bInA := setA[b]
			require.Equalf(t, bInA, aInB,
				"delivery asymmetry: b=%d ∈ delivery(a=%d) is %v but a ∈ delivery(b) is %v",
				b, a, bInA, aInB)
		}
	}
}

// TestMesh_SeverProb_Determinism — same seed + same SeverProb →
// identical Neighbors and NonMeshPeers across builds.
func TestMesh_SeverProb_Determinism(t *testing.T) {
	cluster := []ct.OperatorID{1, 2, 3, 4, 5, 6, 7}
	cfg := ct.MeshConfig{HopDelay: testHopDelay(), SeverProb: 0.25}
	m1 := ct.NewMeshTopology(99, cfg, cluster)
	m2 := ct.NewMeshTopology(99, cfg, cluster)
	total := ct.MeshNode(m1.TotalNodes())
	for node := ct.MeshNode(0); node < total; node++ {
		require.Equal(t, m1.Neighbors(node), m2.Neighbors(node),
			"Neighbors differ for node %d under identical seed+SeverProb", node)
		require.Equal(t, m1.NonMeshPeers(node), m2.NonMeshPeers(node),
			"NonMeshPeers differ for node %d under identical seed+SeverProb", node)
	}
}

// TestMesh_SeverProb_FiltersBothLayers — comparing a SeverProb=0
// baseline with a SeverProb>0 build at the same seed, every peer
// missing from the severed build's delivery set must also have been
// in the baseline (i.e. the only thing severance can do is remove,
// never add) — verified separately for Neighbors (eager layer) and
// NonMeshPeers (gossip layer) to pin that BOTH filters fire.
func TestMesh_SeverProb_FiltersBothLayers(t *testing.T) {
	cluster := []ct.OperatorID{1, 2, 3, 4, 5, 6, 7}
	base := ct.NewMeshTopology(42, ct.MeshConfig{HopDelay: testHopDelay()}, cluster)
	sev := ct.NewMeshTopology(42, ct.MeshConfig{
		HopDelay:  testHopDelay(),
		SeverProb: 0.30,
	}, cluster)
	total := ct.MeshNode(base.TotalNodes())
	asSet := func(s []ct.MeshNode) map[ct.MeshNode]struct{} {
		out := make(map[ct.MeshNode]struct{}, len(s))
		for _, v := range s {
			out[v] = struct{}{}
		}
		return out
	}

	eagerCut, gossipCut := 0, 0
	for node := ct.MeshNode(0); node < total; node++ {
		baseNbr := asSet(base.Neighbors(node))
		sevNbr := asSet(sev.Neighbors(node))
		for nb := range sevNbr {
			_, ok := baseNbr[nb]
			require.True(t, ok, "severance added neighbour %d to node %d", nb, node)
		}
		eagerCut += len(baseNbr) - len(sevNbr)

		baseNonMesh := asSet(base.NonMeshPeers(node))
		sevNonMesh := asSet(sev.NonMeshPeers(node))
		for peer := range sevNonMesh {
			_, ok := baseNonMesh[peer]
			require.True(t, ok, "severance added non-mesh peer %d to node %d", peer, node)
		}
		gossipCut += len(baseNonMesh) - len(sevNonMesh)
	}
	// At p=0.30 over many delivery pairs across 14 nodes, both layers
	// should see at least one cut with near-certainty. Failing either
	// would mean the filter only fires on one side.
	require.Greater(t, eagerCut, 0, "expected ≥1 eager-edge severance at p=0.30")
	require.Greater(t, gossipCut, 0, "expected ≥1 gossip-edge severance at p=0.30")
}

// TestMesh_SeverProb_Zero — SeverProb=0 leaves Neighbors aliasing the
// internal slice (no allocation, no filtering), matching pre-Layer-2
// behaviour exactly.
func TestMesh_SeverProb_Zero(t *testing.T) {
	cluster := []ct.OperatorID{1, 2, 3, 4, 5, 6, 7}
	cfg := ct.MeshConfig{HopDelay: testHopDelay(), SeverProb: 0}
	m := ct.NewMeshTopology(42, cfg, cluster)
	baseline := ct.NewMeshTopology(42, ct.MeshConfig{HopDelay: testHopDelay()}, cluster)
	total := ct.MeshNode(m.TotalNodes())
	for node := ct.MeshNode(0); node < total; node++ {
		require.Equal(t, baseline.Neighbors(node), m.Neighbors(node))
		require.Equal(t, baseline.NonMeshPeers(node), m.NonMeshPeers(node))
	}
}
