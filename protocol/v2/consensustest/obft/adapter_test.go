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
// "OBFT/Rule4/FakeEncryptedPresence". Exercises the full per-rule plumbing
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
		totalRule4 += oo.EvidenceByRule["OBFT/Rule4/FakeEncryptedPresence"]
	}
	require.GreaterOrEqual(t, totalRule4, 3,
		"all 3 honest receivers should fire Rule 4 against byz; got: %v",
		operatorEvidence(out.PerOp))
	t.Logf("Rule 4 fires across cluster: %d", totalRule4)
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

// TestAdapter_ByzWithholdLeader verifies the deepest-layer leader silenced
// pattern: at K = n = 4 with byz=op4 (the L_3 leader), the cluster decides
// at L_2 (op3) without needing the deepest layer.
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
		totalRule1 += oo.EvidenceByRule["OBFT/Rule1/CrossSigning"]
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
		totalRule5 += oo.EvidenceByRule["OBFT/Rule5/FakePlaintextSigma"]
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
		totalRule2 += oo.EvidenceByRule["OBFT/Rule2/LeaderEquivocation"]
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
		totalRule3 += oo.EvidenceByRule["OBFT/Rule3/CrossOnionEquivocation"]
		totalRule3 += oo.EvidenceByRule["OBFT/Rule3/CommitEquivocation"]
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
