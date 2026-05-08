package obft

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDefaultBroadcastBudgetSchedule_K3_TabulatedAtConfigA verifies the K=3
// tabulated default at the production BTT — [1.0, 2.5, 5.5] × 200ms.
func TestDefaultBroadcastBudgetSchedule_K3_TabulatedAtConfigA(t *testing.T) {
	got := DefaultBroadcastBudgetSchedule(3, DefaultBTT)
	want := []time.Duration{
		200 * time.Millisecond,
		500 * time.Millisecond,
		1100 * time.Millisecond,
	}
	require.Equal(t, want, got)
}

// TestDefaultBroadcastBudgetSchedule_K4_TabulatedAtConfigA verifies the K=4
// tabulated default at the production BTT — [1.0, 1.5, 2.5, 5.5] × 200ms.
// This is the production deployment's exact schedule.
func TestDefaultBroadcastBudgetSchedule_K4_TabulatedAtConfigA(t *testing.T) {
	got := DefaultBroadcastBudgetSchedule(4, DefaultBTT)
	want := []time.Duration{
		200 * time.Millisecond,
		300 * time.Millisecond,
		500 * time.Millisecond,
		1100 * time.Millisecond,
	}
	require.Equal(t, want, got)
}

// TestDefaultBroadcastBudgetSchedule_K4_ScalesWithBTT verifies the schedule
// scales linearly when BTT changes — at BTT=400ms the multipliers
// [1.0, 1.5, 2.5, 5.5] produce [400, 600, 1000, 2200]ms.
func TestDefaultBroadcastBudgetSchedule_K4_ScalesWithBTT(t *testing.T) {
	got := DefaultBroadcastBudgetSchedule(4, 400*time.Millisecond)
	want := []time.Duration{
		400 * time.Millisecond,
		600 * time.Millisecond,
		1000 * time.Millisecond,
		2200 * time.Millisecond,
	}
	require.Equal(t, want, got)
}

// TestDefaultBroadcastBudgetSchedule_K7_InterpolatesCleanly verifies the K=7
// interpolation path. With l0=100, deepest=550, K=7: step = (550-100)/6 = 75
// (clean integer division). Multipliers: 100, 175, 250, 325, 400, 475, 550 —
// hits the deepest endpoint exactly.
func TestDefaultBroadcastBudgetSchedule_K7_InterpolatesCleanly(t *testing.T) {
	got := DefaultBroadcastBudgetSchedule(7, DefaultBTT)
	// Multipliers × 200ms / 100 = 2× multipliers as ms.
	want := []time.Duration{
		200 * time.Millisecond,  // 100 × 2
		350 * time.Millisecond,  // 175 × 2
		500 * time.Millisecond,  // 250 × 2
		650 * time.Millisecond,  // 325 × 2
		800 * time.Millisecond,  // 400 × 2
		950 * time.Millisecond,  // 475 × 2
		1100 * time.Millisecond, // 550 × 2 — deepest endpoint exact
	}
	require.Equal(t, want, got)
}

// TestDefaultBroadcastBudgetSchedule_K10_InterpolatesCleanly verifies the
// K=10 interpolation path (n=10 cluster). step = (550-100)/9 = 50 (clean).
func TestDefaultBroadcastBudgetSchedule_K10_InterpolatesCleanly(t *testing.T) {
	got := DefaultBroadcastBudgetSchedule(10, DefaultBTT)
	require.Len(t, got, 10)
	require.Equal(t, 200*time.Millisecond, got[0], "L_0 must be primary endpoint")
	require.Equal(t, 1100*time.Millisecond, got[9], "L_K-1 must be deepest endpoint")

	// Verify monotonically strictly increasing — Validate() depends on this.
	for k := 1; k < len(got); k++ {
		require.Greaterf(t, got[k], got[k-1],
			"budget must be strictly increasing in layer index; got[%d]=%v, got[%d]=%v",
			k, got[k], k-1, got[k-1])
	}
}

// TestDefaultBroadcastBudgetSchedule_K13_OffByOneIsAccepted verifies the
// K=13 interpolation path doesn't hit the deepest endpoint exactly (integer
// division: step = 450/12 = 37, last multiplier = 100 + 12×37 = 544 ≠ 550).
// The strict-monotonic-increasing invariant still holds, which is what
// Validate() requires.
func TestDefaultBroadcastBudgetSchedule_K13_OffByOneIsAccepted(t *testing.T) {
	got := DefaultBroadcastBudgetSchedule(13, DefaultBTT)
	require.Len(t, got, 13)
	require.Equal(t, 200*time.Millisecond, got[0], "L_0 must be primary endpoint exact")
	// L_K-1 = 544 BTT-hundredths × 200ms / 100 = 1088ms (not 1100; integer-
	// division undershoot is intentional). Strict-monotonic still holds.
	require.Equal(t, 1088*time.Millisecond, got[12])
	for k := 1; k < len(got); k++ {
		require.Greater(t, got[k], got[k-1])
	}
}

// TestDefaultBroadcastBudgetSchedule_EndpointConstantsMatchK4 guards against
// drift between primary/deepestBudgetDefaultBTT100 and the K=4 tabulated
// schedule's L_0 / deepest entries. They must match — the K>4 interpolation
// would otherwise jump to a different curve at K=5 vs K=4.
func TestDefaultBroadcastBudgetSchedule_EndpointConstantsMatchK4(t *testing.T) {
	k4 := defaultLayerSchedules[4]
	require.Equal(t, primaryBudgetDefaultBTT100, k4.budgetBTT100[0],
		"primary budget endpoint must match defaultLayerSchedules[4][0]")
	require.Equal(t, deepestBudgetDefaultBTT100, k4.budgetBTT100[len(k4.budgetBTT100)-1],
		"deepest budget endpoint must match defaultLayerSchedules[4][K-1]")
}
