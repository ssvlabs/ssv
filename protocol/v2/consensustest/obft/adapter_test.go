package obft_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
)

// TestAdapter_HealthyAtClusterSizes verifies the adapter runs healthy at
// every SSV-supported cluster size (n=4,7,10,13). Phase 1 plumbs cfg.K /
// cfg.BroadcastBudget / cfg.FetchAt through the adapter; n != 4 was previously
// untested.
func TestAdapter_HealthyAtClusterSizes(t *testing.T) {
	btt := 200 * time.Millisecond
	for _, n := range ct.ClusterSizes {
		n := n
		t.Run(clusterName(n), func(t *testing.T) {
			cfg := ct.SimConfig{
				N:            n,
				Operators:    ct.MakeOperators(n),
				SlotDuration: 12 * time.Second,
				RelayCutoff:  4 * time.Second,
				BTT:          btt,
				Byz:          ct.ByzPattern{Kind: ct.ByzNone},
				Seed:         1,
			}
			out, err := obftadapter.Protocol{}.Run(cfg)
			require.NoError(t, err, "n=%d Run", n)
			require.True(t, out.Decided, "n=%d should decide healthy", n)
			require.Equal(t, 0, out.DecidedRound, "n=%d should decide at L_0 fastest path", n)

			rep := ct.ComputeSafetyReport(out)
			require.True(t, rep.SingleV, "n=%d SingleV: %s", n, rep)
			require.True(t, rep.NoOfflineDoubleV, "n=%d NoOfflineDoubleV: %s", n, rep)
			t.Logf("n=%d K=%d: decided at %v on L_%d", n, ct.DefaultK(cfg.N), out.DecisionTime, out.DecidedRound)
		})
	}
}

// TestAdapter_MultiByzSilentAtN7 runs at n=7 (f=2) with TWO byz operators
// silent as layer leaders; OBFT should still decide via layer fall-through
// to a non-byz-led layer.
func TestAdapter_MultiByzSilentAtN7(t *testing.T) {
	btt := 200 * time.Millisecond
	cfg := ct.SimConfig{
		N:            7,
		Operators:    ct.MakeOperators(7),
		SlotDuration: 12 * time.Second,
		RelayCutoff:  4 * time.Second,
		BTT:          btt,
		Byz: ct.ByzPattern{
			Kind:         ct.ByzSilentLeader,
			ByzOperators: []ct.OperatorID{1, 2},
		},
		Seed: 1,
	}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "n=7 with 2 byz silent leaders should still decide via fall-through")
	require.Greater(t, out.DecidedRound, 1, "should fall through past both byz-led layers (L_0, L_1)")

	rep := ct.ComputeSafetyReport(out)
	require.True(t, rep.SingleV, "SingleV: %s", rep)
	require.True(t, rep.NoOfflineDoubleV, "NoOfflineDoubleV: %s", rep)
	t.Logf("n=7 K=%d 2-byz-silent: decided at %v on L_%d", ct.DefaultK(cfg.N), out.DecisionTime, out.DecidedRound)
}

// TestAdapter_PerRuleEvidence verifies the FakeEncryptedPresence scenario
// fires Rule 4 evidence at honest receivers, classified under
// obftadapter.RuleFakeEncryptedPresence. Exercises the full per-rule plumbing
// path (instance.Evidence() collection, adapter ruleKey mapping, EvidenceByRule
// map propagation) AND validates that Rule 4 detection actually fires under
// the canonical chained-decrypt-fails scenario.
func TestAdapter_PerRuleEvidence(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{
		Kind:         ct.ByzFakeEncryptedPresence,
		ByzOperators: []ct.OperatorID{1},
		Layer:        1,
	}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "FakeEncryptedPresence should fall through to L_1")

	totalRule4 := 0
	for _, oo := range out.PerOp {
		totalRule4 += oo.EvidenceByRule[obftadapter.RuleFakeEncryptedPresence]
	}
	require.GreaterOrEqual(t, totalRule4, 3,
		"all 3 honest receivers should fire Rule 4 against byz; got: %v",
		operatorEvidence(out.PerOp))
	t.Logf("Rule 4 fires across cluster: %d", totalRule4)
}

// TestAdapter_FakeEncryptedPresence_StaysSealed_WhenL0Decides exercises the
// OBFT.md §Slashing evidence / Rule 4 surface-ability limit: "evidence stays
// sealed when NR-quorum doesn't reach at all prior layers". Counterpart to
// TestAdapter_PerRuleEvidence (which exercises the positive detection path
// with byz=op1=L_0-leader silent → NR-quorum at L_0 → chain unlocks).
//
// Setup: byz=op2 (leads L_1 by default rotation, NOT L_0). Byz fakes
// encrypted-presence at L_2 via OverrideCommit. Since byz isn't the L_0
// leader, the healthy op1 broadcasts L_0's bundle and σ-quorum reaches at
// L_0. Production Instance.Resolve halts at the first σ-quorum (L_0) — chain
// decryption at L_1, L_2 never runs, so the garbage at L_2 is never observed
// as garbage. Rule 4 must NOT fire.
//
// This validates the spec's "Rule 4 is best-effort, conditional on slot
// progressing past prior layers' NR-quorum" claim as an in-suite property.
func TestAdapter_FakeEncryptedPresence_StaysSealed_WhenL0Decides(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	// byz=op2 leads L_1 (op[k % N] rotation at K=N=4). Garbage at L_2 is two
	// layers deep — chain unlock requires NR-quorum at BOTH L_0 and L_1.
	cfg.Byz = ct.ByzPattern{
		Kind:         ct.ByzFakeEncryptedPresence,
		ByzOperators: []ct.OperatorID{2},
		Layer:        2,
	}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "L_0 leader honest → cluster must decide at L_0")
	require.Equal(t, 0, out.DecidedRound,
		"healthy L_0 path must hold (byz isn't L_0 leader); got L_%d", out.DecidedRound)

	totalRule4 := 0
	for _, oo := range out.PerOp {
		totalRule4 += oo.EvidenceByRule[obftadapter.RuleFakeEncryptedPresence]
	}
	require.Equal(t, 0, totalRule4,
		"Rule 4 must NOT fire when chain stays sealed (cluster decides at L_0 → no NR-quorum → no chain decryption at L_2 → garbage never observed). got evidence: %v",
		operatorEvidence(out.PerOp))

	rep := ct.ComputeSafetyReport(out)
	require.True(t, rep.SingleV, "SingleV: %s", rep)
	require.True(t, rep.NoOfflineDoubleV, "NoOfflineDoubleV: %s", rep)
	t.Logf("Rule 4 sealed: 0 fires (decided L_%d, chain at L_2 never unlocked)", out.DecidedRound)
}

// TestAdapter_OfflineAggregator_HealthyOneRecon verifies the aggregator
// records exactly one reconstruction on a healthy slot (the decided V) —
// Pigeonhole 2's load-bearing safety claim under all-honest.
func TestAdapter_OfflineAggregator_HealthyOneRecon(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.OfflineAgg.NoOfflineDoubleV,
		"healthy run must satisfy NoOfflineDoubleV: %s", out.OfflineAgg)
	require.Equalf(t, 1, len(out.OfflineAgg.Reconstructions),
		"healthy run should yield exactly one distinct V (the decided V); got: %s",
		out.OfflineAgg)
}

// TestAdapter_MaxMEVFetch_HealthyAtBoundary exercises the OBFT.md §Timing
// budget max-MEV operating point: every leader broadcasts EXACTLY at
// T_broadcast_max_k (LeaderBroadcastOffset = 0 for every layer). Per spec
// §Setting, `B_0 = 1 BTT` decomposes as "0.5 BTT typical-mesh propagation +
// 0.5 BTT convergence buffer" — so the test uses `ConstantDelay{D: BTT/2}`
// (matching P99 ≈ 150ms typical propagation in the spec's Config A) leaving
// the half-BTT convergence buffer intact. Bundle arrives at
// T_broadcast_max_0 + 0.5 BTT = T_commit − 0.5 BTT, comfortably inside the
// acceptance window. Cluster decides at L_0.
//
// Validates: (a) LeaderBroadcastOffset plumbs through DefaultFetchSchedule,
// (b) the spec's B_k = "typical propagation + convergence buffer" decomposition
// at max-MEV fetch holds in simulation.
func TestAdapter_MaxMEVFetch_HealthyAtBoundary(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.WithMaxMEVFetch()
	cfg.Network = ct.ConstantDelay{D: cfg.BTT / 2} // typical-mesh propagation per spec B_k decomposition

	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "max-MEV op-point should decide at L_0 (typical propagation + convergence buffer)")
	require.Equal(t, 0, out.DecidedRound, "max-MEV op-point must decide at L_0 fastest path")

	rep := ct.ComputeSafetyReport(out)
	require.True(t, rep.SingleV, "SingleV: %s", rep)
	require.True(t, rep.NoOfflineDoubleV, "NoOfflineDoubleV: %s", rep)
	t.Logf("MaxMEVFetch op-point: decided at %v on L_%d", out.DecisionTime, out.DecidedRound)
}

// TestAdapter_MaxMEVFetch_FallsThroughWhenConvergenceBufferConsumed exercises
// the spec's pathology: max-MEV fetch (zero broadcast offset) PLUS full-BTT
// propagation (= 1 BTT, no convergence buffer left within B_0). Per spec
// §Setting, this is the boundary where event-ordering between the L_0 arrival
// and the operator's T_commit view can flip the outcome from σ to NR. With
// the test sim's deterministic event ordering (evtPhaseTwoStart at seq N
// fires before evtPhase1Arrival at seq N+M when both land at T_commit), the
// operator commits NR at L_0 → fall-through to L_1.
//
// Validates: (a) the spec's "convergence buffer in B_k" warning is observable
// — at the exact boundary, max-MEV fetch is NOT guaranteed at L_0 under
// full-BTT propagation, (b) the K-layer fall-through correctly handles the
// boundary miss.
func TestAdapter_MaxMEVFetch_FallsThroughWhenConvergenceBufferConsumed(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.WithMaxMEVFetch()
	// Network = ConstantDelay{D: BTT} from DefaultProposerDutyConfig — consumes
	// the full B_0 budget with zero margin.

	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "should still decide via K-layer fall-through")
	require.GreaterOrEqual(t, out.DecidedRound, 1,
		"max-MEV + full-BTT propagation: L_0 should NOT decide (convergence buffer consumed)")

	rep := ct.ComputeSafetyReport(out)
	require.True(t, rep.SingleV, "SingleV: %s", rep)
	t.Logf("MaxMEVFetch + full-BTT propagation: fell through to L_%d at %v", out.DecidedRound, out.DecisionTime)
}

// TestAdapter_ByzWithholdLeader verifies the deepest-layer leader silenced
// pattern: at K = n = 4 with byz=op4 (the L_3 leader), the cluster decides
// at L_0 (op1 leader still broadcasts healthy) without ever reaching the
// silenced deepest layer. Validates that silencing a deeper-layer leader
// is irrelevant when shallower layers succeed — the assertion is
// DecidedRound < 3 (must NOT need L_3), which the L_0 path satisfies.
//
// For the case where ALL layers must be exhausted before the slot misses,
// see TestComparison_Matrix's all-silent scenario.
func TestAdapter_ByzWithholdLeader(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	// Default rotation: L_0=op1, L_1=op2, L_2=op3, L_3=op4. byz=op4 silences L_3.
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzWithholdLeader, ByzOperators: []ct.OperatorID{4}}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "should decide at a non-deepest layer")
	require.Less(t, out.DecidedRound, 3, "should NOT need the silenced deepest layer (L_3)")
}

// TestAdapter_ByzCertWithholding verifies that a byz refusing cert gossip
// doesn't break the slot — honest ops reconstruct independently.
func TestAdapter_ByzCertWithholding(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzCertWithholding, ByzOperators: []ct.OperatorID{4}}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "honest ops should still reconstruct independently of byz cert gossip")
	require.Equal(t, 0, out.DecidedRound, "healthy path holds at L_0")
}

// TestAdapter_ByzCrossSigning verifies Rule 1 evidence fires when byz emits
// BOTH σ AND NR at the same layer. The pattern auto-targets the byz's own
// leader layer (where silent-leader behavior produces a real NR partial); the
// adapter then injects a forged σ entry at that layer. At default rotation,
// op2 leads L_1 — so byz=op2 yields Rule 1 evidence at L_1.
func TestAdapter_ByzCrossSigning(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{
		Kind:         ct.ByzCrossSigning,
		ByzOperators: []ct.OperatorID{2},
	}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)

	totalRule1 := 0
	for _, oo := range out.PerOp {
		totalRule1 += oo.EvidenceByRule[obftadapter.RuleCrossSigning]
	}
	require.GreaterOrEqual(t, totalRule1, 1,
		"at least one honest op should fire Rule 1; got: %v", operatorEvidence(out.PerOp))
	t.Logf("Rule 1 fires across cluster: %d", totalRule1)
}

// TestAdapter_ByzFakePlaintextSigma verifies Rule 5 evidence fires when byz
// emits a plaintext σ at L_0 on a V no Phase-1 leader produced.
func TestAdapter_ByzFakePlaintextSigma(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{
		Kind:         ct.ByzFakePlaintextSigma,
		ByzOperators: []ct.OperatorID{2},
	}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)

	totalRule5 := 0
	for _, oo := range out.PerOp {
		totalRule5 += oo.EvidenceByRule[obftadapter.RuleFakePlaintextSigma]
	}
	require.GreaterOrEqual(t, totalRule5, 1,
		"at least one honest op should fire Rule 5; got: %v", operatorEvidence(out.PerOp))
	t.Logf("Rule 5 fires across cluster: %d", totalRule5)
}

// TestAdapter_LeaderEquivocation_Rule2 verifies Rule 2 evidence fires when
// the L_0 leader emits two distinct Phase-1 bundles (one V to each subset of
// honest receivers). Honest receivers retain both bundles → detect leader
// equivocation → fire Rule 2 with self-contained slashable proof. Uses
// ByzEquivocateAllNR which floods both V's to all honest, ensuring every
// honest sees both bundles.
func TestAdapter_LeaderEquivocation_Rule2(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{
		Kind:         ct.ByzEquivocateAllNR,
		ByzOperators: []ct.OperatorID{1},
	}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)

	totalRule2 := 0
	for _, oo := range out.PerOp {
		totalRule2 += oo.EvidenceByRule[obftadapter.RuleLeaderEquivocation]
	}
	require.GreaterOrEqual(t, totalRule2, 1,
		"at least one honest op should fire Rule 2; got: %v", operatorEvidence(out.PerOp))
	t.Logf("Rule 2 fires across cluster: %d", totalRule2)
}

// TestAdapter_ByzCrossOnionEquivocation verifies Rule 3 per-layer evidence
// fires when byz emits two distinct Commits with different σ at the same layer.
func TestAdapter_ByzCrossOnionEquivocation(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{
		Kind:         ct.ByzCrossOnionEquivocation,
		ByzOperators: []ct.OperatorID{2},
		Layer:        0,
	}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)

	totalRule3 := 0
	for _, oo := range out.PerOp {
		// Rule 3 has two variants: top-level (Layer=-1) and per-layer.
		totalRule3 += oo.EvidenceByRule[obftadapter.RuleCrossOnionEquivocation]
		totalRule3 += oo.EvidenceByRule[obftadapter.RuleCommitEquivocation]
	}
	require.GreaterOrEqual(t, totalRule3, 1,
		"at least one honest op should fire Rule 3; got: %v", operatorEvidence(out.PerOp))
	t.Logf("Rule 3 fires across cluster: %d", totalRule3)
}

// TestAdapter_ByzLateLeaderBroadcast verifies the spec's Class A asymmetric-
// propagation claim: when L_0 leader broadcasts so late that the bundle's
// first-observation lands past T_commit at every honest receiver, the
// cluster falls through to L_1 (whose leader broadcasts on time). Validates
// the per-layer absorption-window mechanism.
func TestAdapter_ByzLateLeaderBroadcast(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	// byz=op1 is the L_0 leader by default rotation.
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzLateLeaderBroadcast, ByzOperators: []ct.OperatorID{1}}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "cluster should fall through to L_1 (honest leader)")
	require.GreaterOrEqual(t, out.DecidedRound, 1,
		"should NOT decide at L_0 (byz bundle past T_commit); got L_%d", out.DecidedRound)

	rep := ct.ComputeSafetyReport(out)
	require.True(t, rep.SingleV, "SingleV: %s", rep)
	require.True(t, rep.NoOfflineDoubleV, "NoOfflineDoubleV: %s", rep)
	t.Logf("Late-L_0-broadcast: decided at %v on L_%d", out.DecisionTime, out.DecidedRound)
}

// TestAdapter_ByzAggregatorBypass_TriggersSafetyDetection is a negative
// test: the byz forges commits claiming distinct identities and a different
// V at L_0. The OfflineAggregator's worst-case-byz-visibility model
// reconstructs both V signatures (the canonical V from honest σ-quorum AND
// the forged V_prime from byz's forged-identity σ partials). NoOfflineDoubleV
// must fire — validates the safety machinery actually detects this class
// of attack.
//
// Calls Run() directly (not RunScenarioOnProtocol) because the safety check
// in RunScenarioOnProtocol panics on NoOfflineDoubleV violations; this test
// inspects ComputeSafetyReport's verdict explicitly.
//
// Tests both byz placements: byz=L_0 leader (op1) and byz=non-leader (op2).
// Both must trigger detection; the bypass forges from all-other-than-self
// to ensure ≥ qV partials on V_prime regardless of byz position.
func TestAdapter_ByzAggregatorBypass_TriggersSafetyDetection(t *testing.T) {
	for _, byzOp := range []ct.OperatorID{1, 2} {
		byzOp := byzOp
		t.Run(fmt.Sprintf("byz=op%d", byzOp), func(t *testing.T) {
			cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
			cfg.Byz = ct.ByzPattern{Kind: ct.ByzAggregatorBypass, ByzOperators: []ct.OperatorID{byzOp}}
			out, err := obftadapter.Protocol{}.Run(cfg)
			require.NoError(t, err)

			rep := ct.ComputeSafetyReport(out)
			require.Falsef(t, rep.NoOfflineDoubleV,
				"byz=op%d: aggregator bypass MUST trigger NoOfflineDoubleV; got: %s", byzOp, rep)
			require.GreaterOrEqualf(t, len(out.OfflineAgg.Reconstructions), 2,
				"byz=op%d: aggregator should reconstruct ≥ 2 distinct V signatures; got: %s",
				byzOp, out.OfflineAgg)
			t.Logf("byz=op%d AggregatorBypass: %s", byzOp, out.OfflineAgg)
		})
	}
}

// TestAdapter_PartialEquivocation_NaturalRecovery verifies the OBFT.md:443
// natural-recovery path: byz leader equivocates 2-1 (V_a → 2 honest, V_b → 1
// honest); σ-pool on V_a = 2 honest σ + leader's σ_L^V(V_a) = 3 = qV at f=1,
// n=4. Slot SUCCEEDS at L_0 with V_a despite equivocation.
//
// Per spec §Phase 2 wire format, Witnesses ship value_root + σ_V (no full V).
// The V_b recipient (op4) cannot use the witnessed σ_L^V(V_a) — it would need
// the V_a bytes which it didn't receive — so op4's σ-pool view at L_0 has
// only V_b partials and op4 falls through. Op2/op3 reach σ-quorum on V_a at
// L_0 and decide; op4 catches up via KindCertificate gossip.
//
// Rule 2 evidence does NOT fire in this scenario: each receiver only sees
// one V via Phase 1, and witnesses don't carry V (only value_root + σ_V).
// This is the deliberate spec trade-off — dropping full V from witnesses
// loses cross-receiver Rule 2 attribution in natural-recovery scenarios.
// Distinct from EquivocateSigmaLockedSplit (1-1-NR slot-miss at OBFT.md:452)
// which has only ≤ 2 partials on each V and therefore reaches no qV.
func TestAdapter_PartialEquivocation_NaturalRecovery(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{
		Kind:         ct.ByzPartialEquivocation,
		ByzOperators: []ct.OperatorID{1},
		Recipients:   []ct.OperatorID{2, 3, 4}, // V_a → op2, op3; V_b → op4
	}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "byz fumbled equivocation: σ-pool on V_a should reach qV naturally")
	require.Equal(t, 0, out.DecidedRound, "should decide at L_0 fastest path with V_a")
	require.Equal(t, "byz-V-A", string(out.DecidedValue),
		"all honest should resolve on V_a (the majority side). got=%q", string(out.DecidedValue))

	rep := ct.ComputeSafetyReport(out)
	require.True(t, rep.SingleV, "Pigeonhole 2: at most one V per layer cluster-wide; SingleV: %s", rep)
	require.True(t, rep.NoOfflineDoubleV, "NoOfflineDoubleV: %s", rep)

	t.Logf("PartialEquivocation 2-1: decided at L_%d with V=%q",
		out.DecidedRound, string(out.DecidedValue))
}

// TestAdapter_LateCommitArrival_ReResolve exercises the spec §Phase 3
// "Re-running on late KindCommit arrivals" recovery path via the NR-quorum
// late-unlock variant ("a late NR partial pushes NR-pool past qEnc at a
// layer that previously had NR-pool short of qEnc → derive the layer-k
// decryption key, unlock chained decryption for layer k+1's σ partials,
// advance the walk past k"). Validates the 1.3 framework
// (EnableLateCommitRerun + evtResolveRerun) salvages a slot that would
// otherwise miss for lack of NR-quorum to unlock chained decryption.
//
// Setup at f=1, n=4, default leader rotation (op_k leads L_{k-1}):
//   - All 3 non-leader hosts are NV at L_0 (op2, op3, op4); ops still
//     σ-emit at L_1+ (host-NV is layer-0-scoped).
//   - op4 is BYZ "delayed commit": its KindCommit at Phase 2 carries an
//     on-protocol NR partial at L_0 plus σ at L_1+, but is dispatched
//     with OverrideOwnCommitDispatchDelay = 1.5·BTT → arrives ~50ms past
//     RoundEndOffset.
//
// Cluster state:
//   - σ at L_0 (cluster-wide): op1's Phase-1 σ_L^V only = 1 < qV=3.
//   - NR at L_0 (cluster-wide): {op2, op3, op4}; with op4 delayed,
//     receivers see only {op2, op3} = 2 < qEnc=3 by RoundEndOffset.
//   - Chain at L_0 stays sealed → L_1 onion entries (where every op
//     σ-emits on V_1) are undecodable.
//
// Initial Resolve fails at L_0 (σ < qV, NR < qEnc). After op4's late
// commit arrives: NR-pool = {op2, op3, op4} = 3 = qEnc → chain key
// for L_0 derived → L_1 onion entries decoded → σ-pool at L_1 reaches
// qV → decide at L_1 via fall-through.
//
// Note: op4 (byz) self-observes its own NR partial in BuildOwnCommit, so
// op4's local state has NR-quorum at RoundEndOffset (own + op2 + op3 = 3).
// op4 decides locally at L_1 in initial Resolve. Other receivers depend on
// either (a) the rerun path after op4's late commit, or (b) cert gossip
// from op4. With EnableLateCommitRerun on, the rerun fires first; cert
// gossip from op4 still arrives but op2/op3/op1 are already decided.
func TestAdapter_LateCommitArrival_ReResolve(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.EnableLateCommitRerun = true
	cfg.Host = ct.HostInvalidForOperators{
		Layer:     0,
		Operators: map[ct.OperatorID]bool{2: true, 3: true, 4: true},
	}
	cfg.Byz = ct.ByzPattern{
		Kind:         ct.ByzDelayedCommit,
		ByzOperators: []ct.OperatorID{4},
	}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	require.True(t, out.Decided, "late-NR re-resolve must salvage the slot")
	// Outcome.DecidedRound is the EARLIEST cluster-wide decision time +
	// layer; that's op4's local decide at L_1 (RoundEndOffset). Receivers
	// rescued via rerun/cert decide later at the same L_1. Both fine — we
	// care that the cluster decides, which validates the recovery path.
	require.Equal(t, 1, out.DecidedRound,
		"cluster should fall through to L_1 via NR-quorum (incl. late op4 NR)")

	rep := ct.ComputeSafetyReport(out)
	require.True(t, rep.SingleV, "SingleV: %s", rep)
	require.True(t, rep.NoOfflineDoubleV, "NoOfflineDoubleV: %s", rep)

	// With rerun enabled, non-byz receivers decide via the rerun path when
	// op4's late commit arrives (~T_commit + BTT + 1.5·BTT = 3900ms at
	// BTT=200). Cert-gossip path (RelayCutoff-adjacent) would not finish in
	// time, but rerun is strictly earlier.
	const rerunPathBudget = 4000 * time.Millisecond // RelayCutoff
	for _, op := range []ct.OperatorID{1, 2, 3} {
		oo, ok := out.PerOp[op]
		require.True(t, ok, "op%d missing from PerOp", op)
		require.True(t, oo.Decided, "op%d should decide", op)
		require.LessOrEqual(t, oo.Time, rerunPathBudget,
			"op%d must decide by RelayCutoff via rerun path; got %v", op, oo.Time)
	}
	t.Logf("Late-NR re-resolve: cluster decided at %v on L_%d; per-op times: op1=%v op2=%v op3=%v op4=%v",
		out.DecisionTime, out.DecidedRound,
		out.PerOp[1].Time, out.PerOp[2].Time, out.PerOp[3].Time, out.PerOp[4].Time)
}

// TestAdapter_LateCommitArrival_NoReResolve_FallsBackToCertGossip is the
// timing counterpart to TestAdapter_LateCommitArrival_ReResolve: same byz
// pattern + host setup, but EnableLateCommitRerun=false. The cluster still
// decides — but via a STRICTLY LATER path (cert gossip from op4's local
// decide) instead of the rerun path that fires when op4's late commit
// arrives at receivers.
//
// Timing differential (the load-bearing assertion of this test):
//   - With rerun: receivers decide at op4's late-commit arrival time
//     (~T_commit + BTT + 1.5·BTT = 3900ms at BTT=200).
//   - Without rerun: receivers decide at op4's cert arrival time
//     (op4's local Resolve at RoundEndOffset = 3850ms; +BTT cert
//     propagation → 4050ms).
//
// The ~150ms differential proves the rerun path is actually load-bearing
// in the positive test, not a coincidence with cert gossip. A regression
// that fires rerun unconditionally (ignoring the flag) would surface as
// receivers deciding at 3900ms in this test — the assertion below would
// fail.
//
// Why outcome.Decided is still true here: op4 (byz) self-observes its own
// NR partial in BuildOwnCommit, so op4's local view has NR-quorum
// (own + op2 + op3 = qEnc=3) at RoundEndOffset → op4 decides at L_1 →
// broadcasts cert → receivers rescue from cert. Suppressing this would
// require extending ByzDelayedCommit to also block cert broadcast (e.g.
// AllowCertificateBroadcast=false), which is plausible future work but
// not needed for the timing-differential assertion this test makes.
func TestAdapter_LateCommitArrival_NoReResolve_FallsBackToCertGossip(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	// cfg.EnableLateCommitRerun stays false (default).
	cfg.Host = ct.HostInvalidForOperators{
		Layer:     0,
		Operators: map[ct.OperatorID]bool{2: true, 3: true, 4: true},
	}
	cfg.Byz = ct.ByzPattern{
		Kind:         ct.ByzDelayedCommit,
		ByzOperators: []ct.OperatorID{4},
	}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)
	// Cluster decides via cert-gossip rescue from op4 even without rerun.
	require.True(t, out.Decided, "cert-gossip rescue from op4's local L_1 decide")
	require.Equal(t, 1, out.DecidedRound, "decide at L_1 via fall-through (op4's local Resolve succeeds)")

	rep := ct.ComputeSafetyReport(out)
	require.True(t, rep.SingleV, "SingleV: %s", rep)
	require.True(t, rep.NoOfflineDoubleV, "NoOfflineDoubleV: %s", rep)

	// Without rerun, non-byz receivers can't decide locally (NR-pool=2<qEnc
	// at their view → chain sealed). They rescue via op4's cert, which is
	// dispatched at op4's local resolve time (RoundEndOffset=3850ms) and
	// arrives at receivers at 3850 + BTT = 4050ms — strictly LATER than
	// the rerun path's 3900ms decide. This timing differential is the
	// in-suite proof that the rerun path is actually load-bearing in the
	// positive test, not a coincidence.
	const rerunPathArrival = 3950 * time.Millisecond
	for _, op := range []ct.OperatorID{1, 2, 3} {
		oo, ok := out.PerOp[op]
		require.True(t, ok, "op%d missing from PerOp", op)
		require.True(t, oo.Decided, "op%d should decide via cert-gossip", op)
		require.Greaterf(t, oo.Time, rerunPathArrival,
			"op%d must decide LATER than the rerun path's ~3900ms (via cert at ~4050ms); got %v",
			op, oo.Time)
	}
	t.Logf("Late-commit no-rerun: cluster decided at %v on L_%d; per-op times: op1=%v op2=%v op3=%v op4=%v",
		out.DecisionTime, out.DecidedRound,
		out.PerOp[1].Time, out.PerOp[2].Time, out.PerOp[3].Time, out.PerOp[4].Time)
}

// TestAdapter_ByzWitnessForgery_TriggersSafetyDetection is the sibling
// negative test to ByzAggregatorBypass: it exercises recordCommitToAggregator's
// Witnesses[] path. Byz emits an extra commit whose Witnesses[] credit ≥ qV
// honest leaders with σ partials on a V_prime at L_1; combined with honest
// σ-quorum on the canonical V at L_0, the OfflineAggregator must report
// NoOfflineDoubleV=false.
//
// Without this test, a regression to the Witnesses crediting at
// obft/events.go's recordCommitToAggregator (the only call site of
// ObserveSigma keyed on w.Leader) would slip past every other test.
//
// Calls Run() directly (not RunScenarioOnProtocol) because the safety check
// in RunScenarioOnProtocol panics on NoOfflineDoubleV violations; this test
// inspects ComputeSafetyReport's verdict explicitly.
func TestAdapter_ByzWitnessForgery_TriggersSafetyDetection(t *testing.T) {
	cfg := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
	cfg.Byz = ct.ByzPattern{Kind: ct.ByzWitnessForgery, ByzOperators: []ct.OperatorID{2}}
	out, err := obftadapter.Protocol{}.Run(cfg)
	require.NoError(t, err)

	rep := ct.ComputeSafetyReport(out)
	require.False(t, rep.NoOfflineDoubleV,
		"witness forgery MUST trigger NoOfflineDoubleV (Witnesses path); got: %s", rep)
	require.GreaterOrEqual(t, len(out.OfflineAgg.Reconstructions), 2,
		"aggregator should reconstruct ≥ 2 distinct V signatures (canonical V at L_0 + V_prime at L_1 via Witnesses); got: %s",
		out.OfflineAgg)
	t.Logf("WitnessForgery: %s", out.OfflineAgg)
}

func clusterName(n int) string {
	switch n {
	case 4:
		return "n=4"
	case 7:
		return "n=7"
	case 10:
		return "n=10"
	case 13:
		return "n=13"
	default:
		return "n=?"
	}
}

func operatorEvidence(perOp map[ct.OperatorID]ct.OperatorOutcome) map[ct.OperatorID]map[string]int {
	out := make(map[ct.OperatorID]map[string]int, len(perOp))
	for op, oo := range perOp {
		if len(oo.EvidenceByRule) > 0 {
			out[op] = oo.EvidenceByRule
		}
	}
	return out
}
