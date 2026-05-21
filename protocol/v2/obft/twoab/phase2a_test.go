package twoab

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyHostValidity_HappyPath(t *testing.T) {
	s := newSim(t, 4)
	op := s.instances[OperatorID(1)]
	require.NoError(t, op.ApplyHostValidity(0, Value("V0"), true))
	valid, recorded := op.HostValidity(0, Value("V0"))
	require.True(t, recorded)
	require.True(t, valid)
}

func TestApplyHostValidity_NotValid(t *testing.T) {
	s := newSim(t, 4)
	op := s.instances[OperatorID(1)]
	require.NoError(t, op.ApplyHostValidity(0, Value("V0"), false))
	valid, recorded := op.HostValidity(0, Value("V0"))
	require.True(t, recorded)
	require.False(t, valid)
}

func TestApplyHostValidity_OverwritePreservesLatest(t *testing.T) {
	s := newSim(t, 4)
	op := s.instances[OperatorID(1)]
	require.NoError(t, op.ApplyHostValidity(0, Value("V0"), true))
	require.NoError(t, op.ApplyHostValidity(0, Value("V0"), false))
	valid, _ := op.HostValidity(0, Value("V0"))
	require.False(t, valid)
}

func TestApplyHostValidity_RejectsBadLayer(t *testing.T) {
	s := newSim(t, 4)
	require.Error(t, s.instances[OperatorID(1)].ApplyHostValidity(99, Value("V0"), true))
}

func TestApplyHostValidity_RejectsEmptyValue(t *testing.T) {
	s := newSim(t, 4)
	require.Error(t, s.instances[OperatorID(1)].ApplyHostValidity(0, Value{}, true))
}

// TestMaybeFirePhase2a_HealthyValuePath: every op has V_0 + host valid →
// emits KindValue at Phase 2a fire.
func TestMaybeFirePhase2a_HealthyValuePath(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	for _, op := range s.allOperators() {
		vm, nv, c, err := s.instances[op].MaybeFirePhase2a()
		require.NoError(t, err)
		require.NotNil(t, vm, "op %d should emit KindValue", op)
		require.Nil(t, nv)
		require.Nil(t, c)
		require.Equal(t, Value("V0"), vm.V)
	}
}

// TestMaybeFirePhase2a_NoValueWhenNoRetention: op without V_0 emits NoValue.
func TestMaybeFirePhase2a_NoValueWhenNoRetention(t *testing.T) {
	s := newSim(t, 4)
	// Deliver V_0 only to ops 1,2,3. Op 4 has nothing retained.
	s.deliverPhase1(0, Value("V0"), []OperatorID{1, 2, 3}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1, 2, 3}, 0, Value("V0"), true)
	vm, nv, c, err := s.instances[OperatorID(4)].MaybeFirePhase2a()
	require.NoError(t, err)
	require.Nil(t, vm)
	require.NotNil(t, nv, "op without V_0 should emit KindNoValue")
	require.Nil(t, c)
}

// TestMaybeFirePhase2a_NoValueWhenHostInvalid: op has V_0 but host says NV.
func TestMaybeFirePhase2a_NoValueWhenHostInvalid(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	// Op 1: host says NV.
	s.applyHostValidityFor([]OperatorID{1}, 0, Value("V0"), false)
	vm, nv, c, err := s.instances[OperatorID(1)].MaybeFirePhase2a()
	require.NoError(t, err)
	require.Nil(t, vm)
	require.NotNil(t, nv)
	require.Nil(t, c)
}

// TestMaybeFirePhase2a_NRDirectWhenEquivocationObserved: op observed ≥2
// distinct V's at L_0 → emits Commit-NRDirect (A8 path).
func TestMaybeFirePhase2a_NRDirectWhenEquivocationObserved(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1Equivocation(0, Value("V_a"), Value("V_b"),
		[]OperatorID{1}, []OperatorID{1}, observedEarly)
	// Op 1 has both V_a and V_b retained.
	vm, nv, c, err := s.instances[OperatorID(1)].MaybeFirePhase2a()
	require.NoError(t, err)
	require.Nil(t, vm)
	require.Nil(t, nv)
	require.NotNil(t, c)
	require.Equal(t, CommitSideNRDirect, c.Side)
}

func TestMaybeFirePhase2a_Idempotent(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), []OperatorID{1}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1}, 0, Value("V0"), true)
	vm1, _, _, err := s.instances[OperatorID(1)].MaybeFirePhase2a()
	require.NoError(t, err)
	vm2, _, _, err := s.instances[OperatorID(1)].MaybeFirePhase2a()
	require.NoError(t, err)
	require.Same(t, vm1, vm2, "second MaybeFirePhase2a should return cached emission")
}

// TestMaybeBuildAndBroadcastUpgrade_A1: NoValue-path op receives V_0 +
// host valid → emits upgrade KindValue.
func TestMaybeBuildAndBroadcastUpgrade_A1(t *testing.T) {
	s := newSim(t, 4)
	// Op 1 doesn't receive V_0 initially.
	s.deliverPhase1(0, Value("V0"), []OperatorID{2, 3, 4}, observedEarly)
	s.applyHostValidityFor([]OperatorID{2, 3, 4}, 0, Value("V0"), true)
	vm, nv, c, err := s.instances[OperatorID(1)].MaybeFirePhase2a()
	require.NoError(t, err)
	require.Nil(t, vm)
	require.NotNil(t, nv)
	require.Nil(t, c)
	// Late delivery of V_0 + host valid to op 1.
	leader := s.leaderAt(0)
	b, err := s.instances[leader].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	require.NoError(t, s.instances[OperatorID(1)].ObservePhase1Bundle(b, observedAfterPhase2a))
	require.NoError(t, s.instances[OperatorID(1)].ApplyHostValidity(0, Value("V0"), true))
	// Upgrade should have fired automatically via the afterStateDelta cascade.
	upgrade, ok := s.instances[OperatorID(1)].OwnValueMsg()
	require.True(t, ok, "op 1 should have emitted upgrade KindValue")
	require.Equal(t, Value("V0"), upgrade.V)
}

// TestMaybeBuildAndBroadcastUpgrade_NotAvailableWhenNoNoValueMsg.
func TestMaybeBuildAndBroadcastUpgrade_NotAvailableWhenNoNoValueMsg(t *testing.T) {
	s := newSim(t, 4)
	// Op 1 hasn't fired Phase 2a yet (so no ownNoValueMsg).
	_, err := s.instances[OperatorID(1)].MaybeBuildAndBroadcastUpgrade()
	require.ErrorIs(t, err, ErrUpgradeNotAvailable)
}
