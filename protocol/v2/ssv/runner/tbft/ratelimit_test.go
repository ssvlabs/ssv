package tbft

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

// ---- Onion limits --------------------------------------------------------

func TestRateLimiter_AllowOnion_FirstAllowedDuplicateRejected(t *testing.T) {
	rl := NewRateLimiter()
	require.NoError(t, rl.AllowOnion(phase0.Slot(100), spectypes.OperatorID(5)))
	err := rl.AllowOnion(phase0.Slot(100), spectypes.OperatorID(5))
	require.ErrorContains(t, err, "already submitted an onion")
}

func TestRateLimiter_AllowOnion_DistinctOperatorsIndependent(t *testing.T) {
	rl := NewRateLimiter()
	require.NoError(t, rl.AllowOnion(phase0.Slot(100), spectypes.OperatorID(1)))
	require.NoError(t, rl.AllowOnion(phase0.Slot(100), spectypes.OperatorID(2)))
}

func TestRateLimiter_AllowOnion_DistinctSlotsIndependent(t *testing.T) {
	rl := NewRateLimiter()
	require.NoError(t, rl.AllowOnion(phase0.Slot(100), spectypes.OperatorID(1)))
	require.NoError(t, rl.AllowOnion(phase0.Slot(101), spectypes.OperatorID(1)))
}

// ---- NonReceipt limits ---------------------------------------------------

func TestRateLimiter_AllowNonReceipt_FirstAllowedDuplicateRejected(t *testing.T) {
	rl := NewRateLimiter()
	require.NoError(t, rl.AllowNonReceipt(phase0.Slot(100), 5, 0))
	err := rl.AllowNonReceipt(phase0.Slot(100), 5, 0)
	require.ErrorContains(t, err, "already submitted a non-receipt")
}

func TestRateLimiter_AllowNonReceipt_DistinctLayersIndependent(t *testing.T) {
	rl := NewRateLimiter()
	require.NoError(t, rl.AllowNonReceipt(phase0.Slot(100), 5, 0))
	require.NoError(t, rl.AllowNonReceipt(phase0.Slot(100), 5, 1))
	require.NoError(t, rl.AllowNonReceipt(phase0.Slot(100), 5, 2))
}

// ---- Candidate limits ----------------------------------------------------

func TestRateLimiter_AllowCandidate_FirstAllowedDuplicateRejected(t *testing.T) {
	rl := NewRateLimiter()
	require.NoError(t, rl.AllowCandidate(phase0.Slot(100), 5, 0))
	err := rl.AllowCandidate(phase0.Slot(100), 5, 0)
	require.ErrorContains(t, err, "already submitted a candidate")
}

// ---- Cross-kind independence --------------------------------------------

func TestRateLimiter_KindsTrackedIndependently(t *testing.T) {
	rl := NewRateLimiter()
	require.NoError(t, rl.AllowOnion(phase0.Slot(100), 5))
	require.NoError(t, rl.AllowNonReceipt(phase0.Slot(100), 5, 0))
	require.NoError(t, rl.AllowCandidate(phase0.Slot(100), 5, 0))
	// All three accepted; tracking is per-kind.
}

// ---- Forget --------------------------------------------------------------

func TestRateLimiter_Forget_ResetsPerSlot(t *testing.T) {
	rl := NewRateLimiter()
	require.NoError(t, rl.AllowOnion(phase0.Slot(100), 1))
	require.NoError(t, rl.AllowNonReceipt(phase0.Slot(100), 1, 0))
	require.NoError(t, rl.AllowCandidate(phase0.Slot(100), 1, 0))

	rl.Forget(phase0.Slot(100))

	// After Forget, the operator can re-submit (e.g. a fresh instance).
	require.NoError(t, rl.AllowOnion(phase0.Slot(100), 1))
	require.NoError(t, rl.AllowNonReceipt(phase0.Slot(100), 1, 0))
	require.NoError(t, rl.AllowCandidate(phase0.Slot(100), 1, 0))
}

func TestRateLimiter_Forget_OnlyAffectsNamedSlot(t *testing.T) {
	rl := NewRateLimiter()
	require.NoError(t, rl.AllowOnion(phase0.Slot(100), 1))
	require.NoError(t, rl.AllowOnion(phase0.Slot(101), 1))

	rl.Forget(phase0.Slot(100))

	// Slot 100 is reset; slot 101 is not.
	require.NoError(t, rl.AllowOnion(phase0.Slot(100), 1))
	require.ErrorContains(t,
		rl.AllowOnion(phase0.Slot(101), 1),
		"already submitted an onion")
}

func TestRateLimiter_Forget_Idempotent(t *testing.T) {
	rl := NewRateLimiter()
	rl.Forget(phase0.Slot(100)) // Forgetting a never-seen slot is fine.
	require.NoError(t, rl.AllowOnion(phase0.Slot(100), 1))
}
