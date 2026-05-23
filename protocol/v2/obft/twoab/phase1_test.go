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
	// There is no T_commit hard wall; ObservePhase1Bundle accepts bundles
	// at any in-slot offset.
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	b, err := s.instances[leader].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	require.NoError(t, s.instances[OperatorID(2)].ObservePhase1Bundle(b, observedAfterPhase2a))
	require.Len(t, s.instances[OperatorID(2)].RetainedBundles(0, leader), 1)
}

// ---------- LWitness tests ----------

// TestBuildPhase1Bundle_PopulatesLWitness verifies that BuildPhase1Bundle
// at L_0 produces a non-empty LWitness signed by the leader on V.
func TestBuildPhase1Bundle_PopulatesLWitness(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	b, err := s.instances[leader].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	require.NotEmpty(t, b.LWitness, "BuildPhase1Bundle at L_0 must populate LWitness")
}

// TestBuildPhase1Bundle_AcquiresSigmaLockAtL0 verifies that signing the
// LWitness acquires the σ-direction EKM lock at L_0 — a subsequent
// BuildPhase1Bundle call with a different V fails ErrSigmaLocked.
func TestBuildPhase1Bundle_AcquiresSigmaLockAtL0(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	_, err := s.instances[leader].BuildPhase1Bundle(0, Value("V_a"))
	require.NoError(t, err)
	_, err = s.instances[leader].BuildPhase1Bundle(0, Value("V_b"))
	require.Error(t, err, "second build on a different V should fail σ-lock")
}

// TestObservePhase1Bundle_PoolsLWitnessOnVerify verifies that receivers
// pool the leader's LWitness into σ-pool[V_0] on successful BLS verify.
func TestObservePhase1Bundle_PoolsLWitnessOnVerify(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	op2 := s.instances[OperatorID(2)]
	b, err := s.instances[leader].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	require.NoError(t, op2.ObservePhase1Bundle(b, observedEarly))
	// σ-pool[V_0] should contain the leader's contribution.
	root := ValueRoot(Value("V0"))
	require.NotEmpty(t, op2.sigmaPool[0][root][leader],
		"σ-pool[V_0] should be seeded with leader's LWitness on bundle observation")
}

// TestObservePhase1Bundle_FakeLWitnessFiresRule5 verifies that a bundle
// with a tampered (non-verifying) LWitness pools nothing into σ-pool
// AND fires Rule 5 (fake plaintext σ) keyed on the leader.
func TestObservePhase1Bundle_FakeLWitnessFiresRule5(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	op2 := s.instances[OperatorID(2)]
	b, err := s.instances[leader].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	// Tamper with the witness — flip a byte.
	b.LWitness[0] ^= 0xff
	require.NoError(t, op2.ObservePhase1Bundle(b, observedEarly))
	// σ-pool should NOT contain the leader (verify failed).
	root := ValueRoot(Value("V0"))
	require.Empty(t, op2.sigmaPool[0][root][leader],
		"tampered LWitness should be rejected; no σ-pool entry")
	// Rule 5 should fire against the leader.
	var found bool
	for _, e := range op2.Evidence() {
		if e.Rule == EvidenceFakePlaintextSigma && e.OperatorID == leader && e.Layer == 0 {
			found = true
			break
		}
	}
	require.True(t, found, "Rule 5 (fake plaintext σ) should fire against the leader on tampered LWitness")
}

// TestObservePhase1Bundle_PreservesLWitnessThroughRetention verifies
// that the deepCopyBundle path inside ObservePhase1Bundle preserves
// LWitness bytes (the defensive copy doesn't drop the field). Pure
// in-memory retention check; for wire-level round-trip see
// wire/wire_test.go.
func TestObservePhase1Bundle_PreservesLWitnessThroughRetention(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	b, err := s.instances[leader].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	op2 := s.instances[OperatorID(2)]
	require.NoError(t, op2.ObservePhase1Bundle(b, observedEarly))
	retained := op2.RetainedBundles(0, leader)
	require.Len(t, retained, 1)
	require.Equal(t, b.LWitness, retained[0].Bundle.LWitness,
		"deep-copied bundle should preserve LWitness")
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
	require.Equal(t, b1.LWitness, b2.LWitness,
		"identical (layer, value) inputs should produce byte-equal LWitness (deterministic signer + cached partial)")
	require.Equal(t, b1.Value, b2.Value)
	require.Equal(t, b1.ClusterID, b2.ClusterID)
}

// ---------- per-layer LWitness tests (K>=3) ----------

// TestBuildPhase1Bundle_PopulatesLWitnessAtAllLayers verifies that every
// layer's leader signs a non-empty LWitness, self-pools it into σ-pool[V_k],
// and acquires the σ-lock at that layer.
func TestBuildPhase1Bundle_PopulatesLWitnessAtAllLayers(t *testing.T) {
	s := newSimWithK(t, 7, 3) // K=3 → layers 0, 1, 2
	for k := 0; k < s.K; k++ {
		leader := s.leaderAt(k)
		v := s.candidates[k]
		b, err := s.instances[leader].BuildPhase1Bundle(k, v)
		require.NoErrorf(t, err, "BuildPhase1Bundle at layer %d", k)
		require.NotEmptyf(t, b.LWitness, "layer %d leader must populate LWitness", k)
		require.Equal(t, k, b.Layer)
		// Self-pooled into σ-pool[k] (head-start for this layer).
		root := ValueRoot(v)
		require.NotEmptyf(t, s.instances[leader].sigmaPool[k][root][leader],
			"σ-pool[%d][V] should be seeded with the leader's own LWitness", k)
		// σ-locked at layer k — a second build on a different V fails.
		_, err = s.instances[leader].BuildPhase1Bundle(k, Value("other"))
		require.Errorf(t, err, "layer %d witness must acquire σ-lock (2nd build on diff V fails)", k)
	}
}

// TestObservePhase1Bundle_PoolsLWitnessAtKGt0 verifies that a receiver
// observing an L_k>0 bundle seeds σ-pool[k] with the leader's witness — the
// head-start that lets a fall-through layer reach σ-quorum faster.
func TestObservePhase1Bundle_PoolsLWitnessAtKGt0(t *testing.T) {
	s := newSimWithK(t, 7, 3)
	const k = 2
	leader := s.leaderAt(k)
	v := s.candidates[k]
	b, err := s.instances[leader].BuildPhase1Bundle(k, v)
	require.NoError(t, err)
	recv := s.instances[OperatorID(5)] // a non-leader
	require.NoError(t, recv.ObservePhase1Bundle(b, observedEarly))
	root := ValueRoot(v)
	require.NotEmpty(t, recv.sigmaPool[k][root][leader],
		"receiver should seed σ-pool[k] with the L_k leader's LWitness")
}

// TestObservePhase1Bundle_FakeLWitnessFiresRule5AtK verifies that a
// tampered LWitness at layer k pools nothing AND fires Rule 5 keyed at
// layer k.
func TestObservePhase1Bundle_FakeLWitnessFiresRule5AtK(t *testing.T) {
	s := newSimWithK(t, 7, 3)
	const k = 2
	leader := s.leaderAt(k)
	v := s.candidates[k]
	b, err := s.instances[leader].BuildPhase1Bundle(k, v)
	require.NoError(t, err)
	b.LWitness[0] ^= 0xff // tamper — fails leader-verify
	recv := s.instances[OperatorID(5)]
	require.NoError(t, recv.ObservePhase1Bundle(b, observedEarly))
	root := ValueRoot(v)
	require.Empty(t, recv.sigmaPool[k][root][leader], "tampered LWitness must not pool")
	var found bool
	for _, e := range recv.Evidence() {
		if e.Rule == EvidenceFakePlaintextSigma && e.OperatorID == leader && e.Layer == k {
			found = true
			break
		}
	}
	require.True(t, found, "Rule 5 must fire at layer k on a fake L_k witness")
}
