package twoab

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWhyNotSigma covers each σ-eligibility verdict the diagnostic reports:
// the structured form canSigmaAtLayer delegates to, and the basis the
// consensustest classifier uses to separate a recoverable stall from the
// validity / σ-split wedges. canSigmaAtLayer == (WhyNotSigma == SigmaEligible)
// is asserted in every case.
func TestWhyNotSigma(t *testing.T) {
	root := ValueRoot(Value("V0"))

	t.Run("no_bundle", func(t *testing.T) {
		s := newSim(t, 4)
		require.Equal(t, SigmaBlockNoBundle, s.instances[2].WhyNotSigma(0))
		require.False(t, s.instances[2].canSigmaAtLayer(0))
		require.Nil(t, s.instances[2].RetainedLeaderValueRoots(0))
	})

	t.Run("host_unrecorded", func(t *testing.T) {
		s := newSim(t, 4)
		s.deliverPhase1(0, Value("V0"), []OperatorID{2}, observedEarly)
		// Bundle retained, but the host verdict has not been applied yet.
		require.Equal(t, SigmaBlockHostUnrecorded, s.instances[2].WhyNotSigma(0))
		require.False(t, s.instances[2].canSigmaAtLayer(0))
		require.Equal(t, [][32]byte{root}, s.instances[2].RetainedLeaderValueRoots(0))
	})

	t.Run("host_invalid", func(t *testing.T) {
		s := newSim(t, 4)
		s.deliverPhase1(0, Value("V0"), []OperatorID{2}, observedEarly)
		s.applyHostValidityFor([]OperatorID{2}, 0, Value("V0"), false)
		require.Equal(t, SigmaBlockHostInvalid, s.instances[2].WhyNotSigma(0))
		require.False(t, s.instances[2].canSigmaAtLayer(0))
	})

	t.Run("eligible", func(t *testing.T) {
		s := newSim(t, 4)
		s.deliverPhase1(0, Value("V0"), []OperatorID{2}, observedEarly)
		s.applyHostValidityFor([]OperatorID{2}, 0, Value("V0"), true)
		require.Equal(t, SigmaEligible, s.instances[2].WhyNotSigma(0))
		require.True(t, s.instances[2].canSigmaAtLayer(0))
		require.Equal(t, [][32]byte{root}, s.instances[2].RetainedLeaderValueRoots(0))
	})

	t.Run("equivocation", func(t *testing.T) {
		s := newSim(t, 4)
		s.deliverPhase1Equivocation(0, Value("Va"), Value("Vb"),
			[]OperatorID{2}, []OperatorID{2}, observedEarly)
		require.Equal(t, SigmaBlockEquivocation, s.instances[2].WhyNotSigma(0))
		require.False(t, s.instances[2].canSigmaAtLayer(0))
		require.Len(t, s.instances[2].RetainedLeaderValueRoots(0), 2)
	})

	t.Run("layer_out_of_range", func(t *testing.T) {
		s := newSim(t, 4)
		require.Equal(t, SigmaBlockNoBundle, s.instances[2].WhyNotSigma(-1))
		require.Nil(t, s.instances[2].RetainedLeaderValueRoots(99))
	})
}

// TestRetainedLeaderValueRoots_ClusterUnion mirrors the all-honest 2-2
// propagation deadlock the consensustest classifier must label a stall:
// leader + one peer hold V0, the other two hold nothing. The cluster-wide
// union of retained leader values is therefore exactly ONE — and one value
// with no host-rejection is what makes classifyDeadlockKind resolve to the
// recoverable stall rather than a σ-split. An honest single-leader cluster
// can never produce ≥ 2 distinct values, so the split bucket is unreachable
// here by construction.
func TestRetainedLeaderValueRoots_ClusterUnion(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	holders := []OperatorID{leader}
	for _, op := range s.allOperators() {
		if op != leader {
			holders = append(holders, op) // first non-leader = the 2nd holder
			break
		}
	}
	s.deliverPhase1(0, Value("V0"), holders, observedEarly)
	s.applyHostValidityFor(holders, 0, Value("V0"), true)

	roots := map[[32]byte]struct{}{}
	for _, op := range s.allOperators() {
		for _, r := range s.instances[op].RetainedLeaderValueRoots(0) {
			roots[r] = struct{}{}
		}
	}
	require.Len(t, roots, 1,
		"all-honest single-leader cluster must retain exactly one value cluster-wide")
}
