package twoab

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Worked-cases catalog per docs/2abOBFT-REDESIGN-PLAN.md §Liveness worked
// cases. Each scenario corresponds to a row in the worked-cases table —
// see the plan for the full state-transition narrative.

// Healthy h_V=4: all 4 ops retain V_0, all host-valid. L_0 σ-quorum.
func TestScenario_HealthyL0Success(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	s.firePhase2aAll()
	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 0, out.Layer)
	require.Equal(t, Value("V0"), out.Value)
}

// Healthy h_V=3: 3 ops retain V_0, 1 doesn't. σ-eligibility reaches qV=3
// from 3 ops; L_0 σ-quorum on V_0.
func TestScenario_HV3SucceedsAtL0(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), []OperatorID{1, 2, 3}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1, 2, 3}, 0, Value("V0"), true)
	s.firePhase2aAll()
	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 0, out.Layer)
	require.Equal(t, Value("V0"), out.Value)
}

// h_V_honest=1 (only leader has V_0 at Phase 1) — V-drops upgrade via
// Phase-1 reflood arriving BEFORE noValuePool reaches qEnc (modeled here
// by orchestrating reflood + host-validity early in the cross-broadcast
// sequence; in real gossipsub, the leader's KindValue propagates
// alongside other ops' KindNoValues, and timing determines whether each
// V-drop processes the leader's bundle before reaching the NR-eligibility
// quorum).
//
// This is the headline case from §Liveness worked cases line 781. The
// test demonstrates the spec-guaranteed-recovery path: 2 honest V-drops
// upgrade, σ-eligibility fires with valuePool ≥ qV, all upgraded ops
// emit Signed → L_0 σ-quorum.
func TestScenario_HV1RecoversViaUpgrade(t *testing.T) {
	s := newSim(t, 4)
	// Op 1 has V_0; ops 2, 3, 4 don't (yet).
	s.deliverPhase1(0, Value("V0"), []OperatorID{1}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1}, 0, Value("V0"), true)
	// Op 1 fires Phase 2a → emits KindValue.
	vmLeader, _, _, err := s.instances[OperatorID(1)].MaybeFirePhase2a()
	require.NoError(t, err)
	require.NotNil(t, vmLeader)
	// Ops 2, 3, 4 fire Phase 2a → emit KindNoValue. But before their
	// NoValues propagate to peers, simulate the Phase-1 reflood reaching
	// ops 2, 3 (op 4 stays drop).
	for _, op := range []OperatorID{2, 3, 4} {
		_, _, _, err := s.instances[op].MaybeFirePhase2a()
		require.NoError(t, err)
	}
	// Pre-NoValue-propagation reflood: deliver V_0 + host validity to
	// ops 2, 3 (modeling the leader's KindValue or Phase-1 reflood
	// arriving before peer NoValues).
	leader := s.leaderAt(0)
	bundle, err := s.instances[leader].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	for _, op := range []OperatorID{2, 3} {
		require.NoError(t, s.instances[op].ObservePhase1Bundle(bundle, observedAfterPhase2a))
		require.NoError(t, s.instances[op].ApplyHostValidity(0, Value("V0"), true))
	}
	// The afterStateDelta cascade inside ApplyHostValidity should have
	// fired the A1 upgrades for ops 2, 3 (preconditions met: ownNoValueMsg
	// set, V_0 retained, host valid).
	for _, op := range []OperatorID{2, 3} {
		_, hadUpgrade := s.instances[op].OwnValueMsg()
		require.True(t, hadUpgrade, "op %d should have emitted upgrade KindValue", op)
	}
	// Cross-broadcast everything: leader's KindValue, V-drops' NoValues,
	// upgrade KindValues from ops 2, 3, and any Phase-2b Commits.
	s.propagatePostPhase2aEmissions()
	// Also cross-broadcast the original Phase-2a emissions (leader's
	// KindValue + NoValues from ops 2, 3, 4) which haven't propagated yet.
	for _, op := range s.allOperators() {
		vm, _ := s.instances[op].OwnValueMsg()
		nv, _ := s.instances[op].OwnNoValueMsg()
		for _, peer := range s.allOperators() {
			if peer == op {
				continue
			}
			if vm != nil {
				require.NoError(t, s.instances[peer].ObserveValueMsg(vm))
			}
			if nv != nil {
				require.NoError(t, s.instances[peer].ObserveNoValueMsg(nv))
			}
		}
	}
	// Propagate any newly-emitted Phase-2b Commits.
	s.propagatePostPhase2aEmissions()
	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 0, out.Layer, "L_0 σ-quorum via upgrades")
	require.Equal(t, Value("V0"), out.Value)
}

// h_V_honest=0: nobody has V_0 → all emit KindNoValue → NR-eligibility
// fires → nr_tag_0-pool reaches qEnc → fall-through to L_1.
func TestScenario_HV0FallsThroughToL1(t *testing.T) {
	s := newSim(t, 4)
	// No L_0 delivery. L_1 delivers.
	s.deliverPhase1(1, s.candidates[1], s.allOperators(), observedEarly)
	s.applyHostValidityAll(1, s.candidates[1], true)
	s.firePhase2aAll()
	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 1, out.Layer)
	require.Equal(t, s.candidates[1], out.Value)
}

// 1-1-1 byz leader equivocation: leader emits 2 distinct V_0 to splitting
// subsets. Receivers observe equivocation → equivocation trigger →
// KindCommit-NR → nr_tag_0-pool reaches qEnc → fall-through to L_1.
func TestScenario_LeaderEquivocationFallsThrough(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1Equivocation(0, Value("V_a"), Value("V_b"),
		s.allOperators(), s.allOperators(), observedEarly)
	// L_1 has a candidate.
	s.deliverPhase1(1, s.candidates[1], s.allOperators(), observedEarly)
	s.applyHostValidityAll(1, s.candidates[1], true)
	s.firePhase2aAll()
	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 1, out.Layer)
}

// Validity-divergence: 3-σV vs 1-NV. 3 ops host-valid on V_0, 1 op
// host-NV. σ-eligibility fires with valuePool=3 ≥ qV. NV-side op's
// side-decision routes to NR (host re-check says NV). 3 σ + 1 NR =
// L_0 σ-quorum on V_0.
func TestScenario_ValidityDivergence3of4Succeeds(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityFor([]OperatorID{1, 2, 3}, 0, Value("V0"), true)
	s.applyHostValidityFor([]OperatorID{4}, 0, Value("V0"), false)
	s.firePhase2aAll()
	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 0, out.Layer, "3-σ + 1-NR reaches L_0 σ-quorum")
	require.Equal(t, Value("V0"), out.Value)
}
