package twoab

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Phase-J scenario catalog (per docs/2abOBFT-IMPL-PLAN.md §Phase J).
//
// Each scenario sets up a known cluster state, drives the protocol from
// Phase-1 through Phase-3, and asserts the expected outcome. Scenarios
// are organized in the order specified in the plan; the comment header
// for each test states the precondition and the expected outcome.

// runHonestPhase2aFor builds + cross-broadcasts verdicts for the given
// subset of operators (byzantine ops aren't called — they "omit
// emission"). Mirrors sim.runPhase2aCanonical but operates on a
// caller-chosen subset.
func runHonestPhase2aFor(s *sim, ops []OperatorID) {
	s.t.Helper()
	for k := 0; k < s.K; k++ {
		for _, op := range ops {
			v, err := s.instances[op].BuildVerdict(k)
			require.NoError(s.t, err, "op %d BuildVerdict layer %d", op, k)
			for _, peer := range ops {
				if peer == op {
					continue
				}
				require.NoError(s.t, s.instances[peer].ObserveVerdict(v))
			}
		}
	}
}

// runHonestPhase2bFor builds + cross-broadcasts onions for the given
// subset of operators.
func runHonestPhase2bFor(s *sim, ops []OperatorID) {
	s.t.Helper()
	for _, op := range ops {
		o, err := s.instances[op].BuildOwnOnion2b()
		require.NoError(s.t, err, "op %d BuildOwnOnion2b", op)
		for _, peer := range ops {
			if peer == op {
				continue
			}
			require.NoError(s.t, s.instances[peer].ObserveOnion2b(o))
		}
	}
}

// ---------- n=4 baseline scenarios ----------

// Healthy slot: every operator retains V_0 at L_0 with host-valid; the
// cluster σ-commits at L_0 and Resolve produces a (L_0, V_0) output.
func TestScenario_HealthyL0Success(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	s.runPhase2aCanonical()
	s.runPhase2bCanonical()

	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 0, out.Layer, "σ-quorum reached at L_0")
	require.Equal(t, Value("V0"), out.Value)
}

// Marginal h_V=3: 3 ops retain V_0; 1 doesn't. σ-eligibility quorum
// reaches (3 σV verdicts == qV); σ-quorum at L_0 succeeds.
func TestScenario_MarginalHV3SucceedsAtL0(t *testing.T) {
	s := newSim(t, 4)
	// Deliver only to ops 1,2,3; op4 doesn't retain.
	s.deliverPhase1(0, Value("V0"), []OperatorID{1, 2, 3}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1, 2, 3}, 0, Value("V0"), true)
	s.runPhase2aCanonical()
	s.runPhase2bCanonical()

	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 0, out.Layer)
	require.Equal(t, Value("V0"), out.Value)
}

// Marginal h_V=2: 2 ops retain V_0, 2 don't. σ-eligibility quorum fails
// (2 < qV=3); NR-pool quorum also fails at L_0 (2 < qEnc=3) so all ops
// route through row 5 → all NR. NR-quorum at L_0 (4 ≥ qEnc) advances
// the walk. L_1 has healthy retention so σ-quorum reaches at L_1.
func TestScenario_MarginalHV2FallsThroughToL1(t *testing.T) {
	s := newSim(t, 4)
	// L_0: only ops 1,2 retain (h_V=2).
	s.deliverPhase1(0, Value("V0"), []OperatorID{1, 2}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1, 2}, 0, Value("V0"), true)
	// L_1: healthy delivery.
	s.deliverPhase1(1, s.candidates[1], s.allOperators(), observedEarly)
	s.applyHostValidityAll(1, s.candidates[1], true)

	s.runPhase2aCanonical()
	s.runPhase2bCanonical()

	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 1, out.Layer, "σ-quorum reached at L_1 after L_0 fall-through")
	require.Equal(t, s.candidates[1], out.Value)
}

// Leader equivocation σ-locked split: byzantine L_0 leader selectively
// delivers V_a to {op2,op3} and V_b to {op1,op4}. No single receiver
// observes both, so Rule 2 doesn't fire — but cluster verdict_pool is
// split 2/2 across two V's, neither reaches qV. Row 5 → all NR → walk
// advances. 2abOBFT recovers via NR-fall-through where bare OBFT would
// slot-miss.
func TestScenario_LeaderEquivocationFallsThrough(t *testing.T) {
	s := newSim(t, 4)
	// Build V_a and V_b from the byzantine L_0 leader; deliver each to
	// disjoint halves of the cluster.
	s.deliverPhase1Equivocation(0,
		Value("V_a"), Value("V_b"),
		[]OperatorID{2, 3}, // V_a recipients
		[]OperatorID{1, 4}, // V_b recipients
		observedEarly,
	)
	s.applyHostValidityFor([]OperatorID{2, 3}, 0, Value("V_a"), true)
	s.applyHostValidityFor([]OperatorID{1, 4}, 0, Value("V_b"), true)
	// L_1 healthy fall-through.
	s.deliverPhase1(1, s.candidates[1], s.allOperators(), observedEarly)
	s.applyHostValidityAll(1, s.candidates[1], true)

	s.runPhase2aCanonical()
	s.runPhase2bCanonical()

	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 1, out.Layer, "split locks at L_0 → NR-fall-through to L_1")
	require.Equal(t, s.candidates[1], out.Value)
}

// h_V=1 selective-delivery: only 1 receiver retains V_0 at L_0. nr_pool
// quorum (3 ≥ qEnc) drives row 2 NR at sign time. Walk advances to L_1
// where healthy retention succeeds.
func TestScenario_HV1FallsThrough(t *testing.T) {
	s := newSim(t, 4)
	// Byzantine L_0 leader (op1) delivers only to op2 — op1 itself does
	// not self-observe (modeling a byzantine that withholds its own
	// retention).
	s.deliverPhase1(0, Value("V0"), []OperatorID{2}, observedEarly)
	s.applyHostValidityFor([]OperatorID{2}, 0, Value("V0"), true)
	// L_1 healthy.
	s.deliverPhase1(1, s.candidates[1], s.allOperators(), observedEarly)
	s.applyHostValidityAll(1, s.candidates[1], true)

	s.runPhase2aCanonical()
	s.runPhase2bCanonical()

	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 1, out.Layer)
}

// Validity-divergence 3-of-4 at L_0: all 4 retain V_0; 3 hosts say
// valid, 1 says NV. verdict_pool[V_0] = 3 ≥ qV → σ-eligibility met. The
// 3 valid ops σ-emit on V_0 (row 3); the NV op emits NR (row 4). σ-pool
// at L_0 has 3 partials → σ-quorum reaches; reconstruction succeeds.
func TestScenario_ValidityDivergence3of4Succeeds(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	// 3 valid + 1 NV.
	s.applyHostValidityFor([]OperatorID{1, 2, 3}, 0, Value("V0"), true)
	s.applyHostValidityFor([]OperatorID{4}, 0, Value("V0"), false)

	s.runPhase2aCanonical()
	s.runPhase2bCanonical()

	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 0, out.Layer, "σ-quorum at L_0 even with one NV verdict")
	require.Equal(t, Value("V0"), out.Value)
}

// Validity-divergence 2-2 at L_0 with no fallback retention: all 4
// retain V_0; 2 hosts say valid, 2 say NV. Neither σ-eligibility nor
// nr_pool reaches qV/qEnc → row 5 → all NR. NR-quorum advances; but no
// retention at L_1+ so subsequent layers also row 5 / NR until the
// walk completes with no σ-quorum. Slot misses cleanly per the spec's
// algebraic 2-2 bound at n=4.
func TestScenario_ValidityDivergence2of2MissesCleanly(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityFor([]OperatorID{1, 2}, 0, Value("V0"), true)
	s.applyHostValidityFor([]OperatorID{3, 4}, 0, Value("V0"), false)
	// No L_1+ retention.

	s.runPhase2aCanonical()
	s.runPhase2bCanonical()

	_, errs := s.resolveAll()
	for op, err := range errs {
		require.ErrorIs(t, err, ErrNoQuorum, "op %d expected slot miss", op)
	}
}

// 2-1-byz-defect at every layer: 2 honest retain V, 1 honest doesn't
// retain (didn't receive), 1 byzantine omits Phase-2a + Phase-2b
// emission entirely. σ-eligibility (2 σV) < qV; nr_pool (1 from honest
// no-V op) < qEnc; row 5 NR. NR-quorum at L_0 just barely reaches
// (3 honest NR partials = qEnc) — walk advances. Same pattern repeats
// per layer; at the deepest layer no NR-emission so the walk completes
// without σ-quorum and the slot misses. Documented regression vs full
// 4-of-4 healthy.
func TestScenario_TwoOneByzantineDefectMisses(t *testing.T) {
	s := newSim(t, 4)
	honest := []OperatorID{1, 2, 3} // op4 is byzantine, omits emissions
	// Per layer: 2 honest retain V, 1 doesn't.
	for k := 0; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], []OperatorID{1, 2}, observedEarly)
		s.applyHostValidityFor([]OperatorID{1, 2}, k, s.candidates[k], true)
	}
	runHonestPhase2aFor(s, honest)
	runHonestPhase2bFor(s, honest)

	// Honest ops Resolve — slot misses (no σ-quorum at any layer).
	for _, op := range honest {
		_, err := s.instances[op].Resolve()
		require.ErrorIs(t, err, ErrNoQuorum, "op %d expected slot miss", op)
	}
}

// Verdict-equivocation recovery under marginal h_V=3: byzantine op2
// equivocates by broadcasting both σV and NR verdicts at L_0; every
// receiver observes both → marks op2 as equivocator → treat-as-null
// excludes op2 from convergence. Remaining σV pool (2 ops) < qV → row 5
// → NR-fall-through to L_1 where healthy retention restores σ-quorum.
func TestScenario_VerdictEquivocationRecoversViaTreatAsNull(t *testing.T) {
	s := newSim(t, 4)
	// Marginal h_V=3 at L_0 (op4 doesn't retain).
	s.deliverPhase1(0, Value("V0"), []OperatorID{1, 2, 3}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1, 2, 3}, 0, Value("V0"), true)
	// L_1 healthy fall-through.
	s.deliverPhase1(1, s.candidates[1], s.allOperators(), observedEarly)
	s.applyHostValidityAll(1, s.candidates[1], true)

	// Build op2's honest σV verdict + a byzantine NR variant; both are
	// cluster-broadcast so every receiver flags op2 as equivocator.
	vSigma, err := s.instances[2].BuildVerdict(0)
	require.NoError(t, err)
	require.Equal(t, VerdictSigmaV, vSigma.Kind)
	vNR := &Verdict{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 2,
		Height:     s.cfg.Height,
		Layer:      0,
		Kind:       VerdictNR,
	}
	receivers := []OperatorID{1, 3, 4} // op2 self
	s.deliverVerdictEquivocation(2, 0, vSigma, vNR, receivers, receivers)

	// Other honest verdicts at L_0 + L_1, and op2's L_1 verdict, all
	// canonical.
	for _, op := range []OperatorID{1, 3, 4} {
		v, err := s.instances[op].BuildVerdict(0)
		require.NoError(t, err)
		for _, peer := range s.allOperators() {
			if peer == op {
				continue
			}
			require.NoError(t, s.instances[peer].ObserveVerdict(v))
		}
	}
	for _, op := range s.allOperators() {
		v, err := s.instances[op].BuildVerdict(1)
		require.NoError(t, err)
		for _, peer := range s.allOperators() {
			if peer == op {
				continue
			}
			require.NoError(t, s.instances[peer].ObserveVerdict(v))
		}
	}
	// Skip L_2+ verdicts — convergence at those layers without retention
	// just routes to row 5 / NR; BuildVerdict at no-retention layers
	// produces NR verdicts uniformly.
	for k := 2; k < s.K; k++ {
		for _, op := range s.allOperators() {
			v, err := s.instances[op].BuildVerdict(k)
			require.NoError(t, err)
			for _, peer := range s.allOperators() {
				if peer == op {
					continue
				}
				require.NoError(t, s.instances[peer].ObserveVerdict(v))
			}
		}
	}

	s.runPhase2bCanonical()
	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 1, out.Layer, "treat-as-null excludes equivocator, fall-through to L_1")
	require.Equal(t, s.candidates[1], out.Value)

	// Confirm Rule 6a fired at each receiver.
	for _, op := range receivers {
		fired := false
		for _, e := range s.instances[op].Evidence() {
			if e.Rule == EvidenceVerdictEquivocation && e.OperatorID == 2 && e.Layer == 0 {
				fired = true
				break
			}
		}
		require.True(t, fired, "op %d should have flagged op2 Rule 6a", op)
	}
}

// Late deepest-layer broadcast → Phase-2a re-flood recovery: L_3 leader
// broadcasts after T_accept_max; receivers initially auth-only-retain
// (which doesn't drive σV verdicts). A subsequent re-flood observation
// within the accept window promotes auth-only → regular, enabling σV
// verdicts at L_3. Shallower layers (L_0..L_2) lack retention so the
// walk progresses via NR-fall-through; σ-quorum reaches at L_3.
func TestScenario_LateDeepestLayerAuthOnlyToRegularRecovery(t *testing.T) {
	s := newSim(t, 4)
	deepest := s.K - 1
	leader := s.leaderAt(deepest)

	// First observation: L_3 leader broadcasts late (auth-only).
	b, err := s.instances[leader].BuildPhase1Bundle(deepest, s.candidates[deepest])
	require.NoError(t, err)
	for _, op := range s.allOperators() {
		require.NoError(t, s.instances[op].ObservePhase1Bundle(b, observedAuthOnly))
	}
	// Re-flood within the accept window promotes auth-only → regular at
	// every receiver.
	for _, op := range s.allOperators() {
		require.NoError(t, s.instances[op].ObservePhase1Bundle(b, observedEarly))
	}
	// Host applies validity for V_{K-1} at every receiver.
	s.applyHostValidityAll(deepest, s.candidates[deepest], true)

	s.runPhase2aCanonical()
	s.runPhase2bCanonical()

	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, deepest, out.Layer, "σ-quorum reaches at deepest layer after re-flood promotion")
	require.Equal(t, s.candidates[deepest], out.Value)
}

// ---------- n=7, f=2 scenarios ----------

// 4-3 validity-divergence at n=7: 4 hosts say valid, 3 say NV.
// verdict_pool[V_0] = 4 < qV=5; nr_pool = 3 < qEnc=5; row 5 → all NR.
// NR-pool at L_0 = 7 ≥ qEnc; walk advances. Healthy L_1 retention →
// σ-quorum at L_1. Verifies the spec claim that 2ab recovers validity-
// divergence MAJORITY (not just 4-of-4) at n > 4, not only at n=4.
func TestScenario_N7_ValidityDivergence4of3FallsThrough(t *testing.T) {
	s := newSimWithF(t, 7, 2)
	require.Equal(t, 5, s.cfg.QV())
	require.Equal(t, 5, s.cfg.QEnc())

	allOps := s.allOperators()
	s.deliverPhase1(0, Value("V0"), allOps, observedEarly)
	// 4 valid, 3 NV.
	valid := []OperatorID{1, 2, 3, 4}
	nv := []OperatorID{5, 6, 7}
	s.applyHostValidityFor(valid, 0, Value("V0"), true)
	s.applyHostValidityFor(nv, 0, Value("V0"), false)
	// L_1 healthy.
	s.deliverPhase1(1, s.candidates[1], allOps, observedEarly)
	s.applyHostValidityAll(1, s.candidates[1], true)

	s.runPhase2aCanonical()
	s.runPhase2bCanonical()

	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 1, out.Layer, "n=7 4-3 split → NR-fall-through → σ at L_1")
	require.Equal(t, s.candidates[1], out.Value)
}
