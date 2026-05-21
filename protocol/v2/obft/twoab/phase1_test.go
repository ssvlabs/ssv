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
	op2 := s.instances[OperatorID(2)]
	for _, v := range []Value{Value("V_a"), Value("V_b"), Value("V_c")} {
		b, err := s.instances[leader].BuildPhase1Bundle(0, v)
		require.NoError(t, err)
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
