package twoab

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildPhase1Bundle_HealthyLeader(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	b, err := s.instances[leader].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	require.Equal(t, leader, b.OperatorID)
	require.Equal(t, 0, b.Layer)
	require.Equal(t, Value("V0"), b.Value)
}

func TestBuildPhase1Bundle_RejectsNonLeader(t *testing.T) {
	s := newSim(t, 4)
	nonLeader := OperatorID(99)
	_ = nonLeader
	// op4 is not the L_0 leader at K=2.
	_, err := s.instances[OperatorID(4)].BuildPhase1Bundle(0, Value("V0"))
	require.ErrorIs(t, err, ErrNotLeader)
}

func TestBuildPhase1Bundle_RejectsEmptyValue(t *testing.T) {
	s := newSim(t, 4)
	_, err := s.instances[s.leaderAt(0)].BuildPhase1Bundle(0, Value{})
	require.ErrorIs(t, err, ErrEmptyValue)
}

func TestBuildPhase1Bundle_RejectsLayerOutOfRange(t *testing.T) {
	s := newSim(t, 4)
	_, err := s.instances[s.leaderAt(0)].BuildPhase1Bundle(99, Value("V0"))
	require.ErrorIs(t, err, ErrLayerOutOfRange)
}

func TestObservePhase1Bundle_StoresFirstObservation(t *testing.T) {
	s := newSim(t, 4)
	b := s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	for _, op := range s.allOperators() {
		retained := s.instances[op].RetainedBundles(0, b.OperatorID)
		require.Len(t, retained, 1)
		require.Equal(t, Value("V0"), retained[0].Bundle.Value)
	}
}

func TestObservePhase1Bundle_IdenticalRebroadcastIsSilentDedup(t *testing.T) {
	s := newSim(t, 4)
	b := s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	// Re-broadcast same bundle.
	for _, op := range s.allOperators() {
		require.NoError(t, s.instances[op].ObservePhase1Bundle(b, observedEarly))
	}
	for _, op := range s.allOperators() {
		retained := s.instances[op].RetainedBundles(0, b.OperatorID)
		require.Len(t, retained, 1)
	}
}

func TestObservePhase1Bundle_LeaderEquivocationFiresRule2(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1Equivocation(0, Value("V_a"), Value("V_b"),
		[]OperatorID{1, 2, 3, 4}, []OperatorID{1, 2, 3, 4}, observedEarly)
	// Every op should have 2 retained bundles + Rule 2 evidence.
	for _, op := range s.allOperators() {
		retained := s.instances[op].RetainedBundles(0, s.leaderAt(0))
		require.Len(t, retained, 2)
		ev := s.instances[op].Evidence()
		require.NotEmpty(t, ev)
		foundRule2 := false
		for _, e := range ev {
			if e.Rule == EvidenceLeaderEquivocation {
				foundRule2 = true
				require.NotNil(t, e.LeaderEquivocation)
			}
		}
		require.True(t, foundRule2, "op %d should have Rule 2 evidence", op)
	}
}

func TestObservePhase1Bundle_ThirdDistinctBundleSilentlyDropped(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	// Cap at 2 distinct V's. Build 3 bundles, deliver all to op2.
	// Byz-equivocation: leader signs all three via direct signer access
	// (BuildPhase1Bundle would σ-lock on the first V and reject the rest).
	op2 := s.instances[OperatorID(2)]
	for _, v := range []Value{Value("V_a"), Value("V_b"), Value("V_c")} {
		b := s.buildByzEquivocatingBundle(leader, 0, v)
		require.NoError(t, op2.ObservePhase1Bundle(b, observedEarly))
	}
	retained := op2.RetainedBundles(0, leader)
	require.Len(t, retained, 2) // capped at 2
}

func TestObservePhase1Bundle_AcceptsLateBundleNoErrLatePhase1(t *testing.T) {
	// v4 has no T_commit hard wall; ObservePhase1Bundle accepts bundles
	// at any in-slot offset.
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	b, err := s.instances[leader].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	require.NoError(t, s.instances[OperatorID(2)].ObservePhase1Bundle(b, observedAfterPhase2a))
	require.Len(t, s.instances[OperatorID(2)].RetainedBundles(0, leader), 1)
}

// ---------- Op3 L0Witness tests ----------

// TestBuildPhase1Bundle_PopulatesL0Witness verifies that BuildPhase1Bundle
// at L_0 produces a non-empty L0Witness signed by the leader on V.
func TestBuildPhase1Bundle_PopulatesL0Witness(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	b, err := s.instances[leader].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	require.NotEmpty(t, b.L0Witness, "Op3: BuildPhase1Bundle at L_0 must populate L0Witness")
}

// TestBuildPhase1Bundle_AcquiresSigmaLockAtL0 verifies that signing the
// L0Witness acquires the σ-direction EKM lock at L_0 — a subsequent
// BuildPhase1Bundle call with a different V fails ErrSigmaLocked.
func TestBuildPhase1Bundle_AcquiresSigmaLockAtL0(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	_, err := s.instances[leader].BuildPhase1Bundle(0, Value("V_a"))
	require.NoError(t, err)
	_, err = s.instances[leader].BuildPhase1Bundle(0, Value("V_b"))
	require.Error(t, err, "second build on a different V should fail σ-lock")
}

// TestObservePhase1Bundle_PoolsL0WitnessOnVerify verifies that receivers
// pool the leader's L0Witness into σ-pool[V_0] on successful BLS verify.
func TestObservePhase1Bundle_PoolsL0WitnessOnVerify(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	op2 := s.instances[OperatorID(2)]
	b, err := s.instances[leader].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	require.NoError(t, op2.ObservePhase1Bundle(b, observedEarly))
	// σ-pool[V_0] should contain the leader's contribution.
	root := ValueRoot(Value("V0"))
	require.NotEmpty(t, op2.sigmaPool[0][root][leader],
		"Op3: σ-pool[V_0] should be seeded with leader's L0Witness on bundle observation")
}

// TestObservePhase1Bundle_FakeL0WitnessFiresRule5 verifies that a bundle
// with a tampered (non-verifying) L0Witness pools nothing into σ-pool
// AND fires Rule 5 (fake plaintext σ) keyed on the leader.
func TestObservePhase1Bundle_FakeL0WitnessFiresRule5(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	op2 := s.instances[OperatorID(2)]
	b, err := s.instances[leader].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	// Tamper with the witness — flip a byte.
	b.L0Witness[0] ^= 0xff
	require.NoError(t, op2.ObservePhase1Bundle(b, observedEarly))
	// σ-pool should NOT contain the leader (verify failed).
	root := ValueRoot(Value("V0"))
	require.Empty(t, op2.sigmaPool[0][root][leader],
		"tampered L0Witness should be rejected; no σ-pool entry")
	// Rule 5 should fire against the leader.
	var found bool
	for _, e := range op2.Evidence() {
		if e.Rule == EvidenceFakePlaintextSigma && e.OperatorID == leader && e.Layer == 0 {
			found = true
			break
		}
	}
	require.True(t, found, "Rule 5 (fake plaintext σ) should fire against the leader on tampered L0Witness")
}

// TestObservePhase1Bundle_PreservesL0WitnessThroughRetention verifies
// that the deepCopyBundle path inside ObservePhase1Bundle preserves
// L0Witness bytes (the defensive copy doesn't drop the field). Pure
// in-memory retention check; for wire-level round-trip see
// wire/wire_test.go.
func TestObservePhase1Bundle_PreservesL0WitnessThroughRetention(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	b, err := s.instances[leader].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	op2 := s.instances[OperatorID(2)]
	require.NoError(t, op2.ObservePhase1Bundle(b, observedEarly))
	retained := op2.RetainedBundles(0, leader)
	require.Len(t, retained, 1)
	require.Equal(t, b.L0Witness, retained[0].Bundle.L0Witness,
		"deep-copied bundle should preserve L0Witness")
}

// TestBuildPhase1Bundle_IdempotentOnSameValue verifies the docstring
// idempotency claim: calling BuildPhase1Bundle twice with the same
// (layer, value) returns byte-equal bundles and doesn't error on the
// second call (cached partial via i.ownPartials[0], idempotent
// transitionToSigma on same value).
func TestBuildPhase1Bundle_IdempotentOnSameValue(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	b1, err := s.instances[leader].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	b2, err := s.instances[leader].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err, "second build on same value should succeed (idempotent)")
	require.Equal(t, b1.L0Witness, b2.L0Witness,
		"identical (layer, value) inputs should produce byte-equal L0Witness (deterministic signer + cached partial)")
	require.Equal(t, b1.Value, b2.Value)
	require.Equal(t, b1.ClusterID, b2.ClusterID)
}
