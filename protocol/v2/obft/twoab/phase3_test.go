package twoab

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResolve_HealthyClusterReachesSigmaAtL0: all ops emit Signed; Resolve
// reconstructs at L_0 with V_0.
func TestResolve_HealthyClusterReachesSigmaAtL0(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	s.firePhase2aAll()
	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 0, out.Layer, "σ-quorum reached at L_0")
	require.Equal(t, Value("V0"), out.Value)
}

// TestResolve_FallThroughToL1WhenNoOneHasV0: h_V_honest=0 → all emit NR
// → nr_tag_0-pool reaches qEnc → fall-through to L_1.
func TestResolve_FallThroughToL1WhenNoOneHasV0(t *testing.T) {
	s := newSim(t, 4)
	// Nobody has V_0 at L_0. But L_1 leader delivers and host says valid.
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

// TestResolve_NoVAtAnyLayerReturnsNoQuorum.
func TestResolve_NoVAtAnyLayerReturnsNoQuorum(t *testing.T) {
	s := newSim(t, 4)
	// Nothing at L_0, nothing at L_1.
	s.firePhase2aAll()
	_, errs := s.resolveAll()
	for op, err := range errs {
		require.True(t, errors.Is(err, ErrNoQuorum), "op %d should get NoQuorum, got %v", op, err)
	}
}

// TestResolve_LeaderEquivocationFallsThrough: leader equivocates at L_0
// → equivocation trigger fires for all → all emit NR-direct or Phase-2b
// NR → fall-through to L_1.
func TestResolve_LeaderEquivocationFallsThrough(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1Equivocation(0, Value("V_a"), Value("V_b"),
		s.allOperators(), s.allOperators(), observedEarly)
	// L_1 also delivers (so fall-through has a target).
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

// TestResolve_Idempotent.
func TestResolve_Idempotent(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	s.firePhase2aAll()
	out1, err := s.instances[OperatorID(1)].Resolve()
	require.NoError(t, err)
	out2, err := s.instances[OperatorID(1)].Resolve()
	require.NoError(t, err)
	require.Equal(t, out1.Layer, out2.Layer)
	require.Equal(t, out1.Value, out2.Value)
}

func TestBuildCertificate_HappyPath(t *testing.T) {
	s := newSim(t, 4)
	cert, err := s.instances[OperatorID(1)].BuildCertificate(&Output{
		Layer:     0,
		Value:     Value("V0"),
		Signature: Signature("sig"),
	})
	require.NoError(t, err)
	require.Equal(t, Value("V0"), cert.Value)
}

func TestBuildCertificate_RejectsNil(t *testing.T) {
	s := newSim(t, 4)
	_, err := s.instances[OperatorID(1)].BuildCertificate(nil)
	require.Error(t, err)
}

func TestObserveCertificate_HappyPath(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	s.firePhase2aAll()
	out, err := s.instances[OperatorID(1)].Resolve()
	require.NoError(t, err)
	cert, err := s.instances[OperatorID(1)].BuildCertificate(out)
	require.NoError(t, err)
	require.NoError(t, s.instances[OperatorID(2)].ObserveCertificate(cert))
	require.NotNil(t, s.instances[OperatorID(2)].RetainedCertificate())
}

func TestRetainedCertificate_NilBeforeObserve(t *testing.T) {
	s := newSim(t, 4)
	require.Nil(t, s.instances[OperatorID(1)].RetainedCertificate())
}
