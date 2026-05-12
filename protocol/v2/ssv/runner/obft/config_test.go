package obft

import (
	"strconv"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

// TestDefaultBroadcastBudgetSchedule_K3_TabulatedAtConfigA verifies the K=3
// tabulated default at the production BTT — [1.0·BTT, 2.5·BTT, T_commit] =
// [200, 500, 3400]ms at Config A.
func TestDefaultBroadcastBudgetSchedule_K3_TabulatedAtConfigA(t *testing.T) {
	got, err := DefaultBroadcastBudgetSchedule(3, DefaultBTT, DefaultTCommit)
	require.NoError(t, err)
	want := []time.Duration{
		200 * time.Millisecond,
		500 * time.Millisecond,
		DefaultTCommit,
	}
	require.Equal(t, want, got)
}

// TestDefaultBroadcastBudgetSchedule_K4_TabulatedAtConfigA verifies the K=4
// tabulated default at the production BTT — [1·BTT, 1.5·BTT, 2.5·BTT,
// T_commit] = [200, 300, 500, 3400]ms at Config A. This is the production
// deployment's exact schedule.
func TestDefaultBroadcastBudgetSchedule_K4_TabulatedAtConfigA(t *testing.T) {
	got, err := DefaultBroadcastBudgetSchedule(4, DefaultBTT, DefaultTCommit)
	require.NoError(t, err)
	want := []time.Duration{
		200 * time.Millisecond,
		300 * time.Millisecond,
		500 * time.Millisecond,
		DefaultTCommit,
	}
	require.Equal(t, want, got)
}

// TestDefaultBroadcastBudgetSchedule_K4_ScalesWithBTT verifies the shallow
// schedule scales linearly when BTT changes — at BTT=400ms the multipliers
// [1.0, 1.5, 2.5] produce [400, 600, 1000]ms shallow; deepest stays anchored
// at T_commit (not BTT-scaled).
func TestDefaultBroadcastBudgetSchedule_K4_ScalesWithBTT(t *testing.T) {
	got, err := DefaultBroadcastBudgetSchedule(4, 400*time.Millisecond, DefaultTCommit)
	require.NoError(t, err)
	want := []time.Duration{
		400 * time.Millisecond,
		600 * time.Millisecond,
		1000 * time.Millisecond,
		DefaultTCommit,
	}
	require.Equal(t, want, got)
}

// TestDefaultBroadcastBudgetSchedule_K7_InterpolatesCleanly verifies the K=7
// interpolation: first three shallow layers stay at [1·BTT, 1.5·BTT, 2.5·BTT]
// = [200, 300, 500]ms; intermediate layers (k=3..K-2) interpolate from
// 2.5·BTT (at L_2) to T_commit (at L_{K-1}) in duration space. Deepest is
// T_commit exact.
func TestDefaultBroadcastBudgetSchedule_K7_InterpolatesCleanly(t *testing.T) {
	got, err := DefaultBroadcastBudgetSchedule(7, DefaultBTT, DefaultTCommit)
	require.NoError(t, err)
	require.Len(t, got, 7)
	require.Equal(t, 200*time.Millisecond, got[0], "L_0 = 1·BTT")
	require.Equal(t, 300*time.Millisecond, got[1], "L_1 = 1.5·BTT")
	require.Equal(t, 500*time.Millisecond, got[2], "L_2 = 2.5·BTT")
	require.Equal(t, DefaultTCommit, got[6], "L_K-1 = T_commit (earliest possible)")
	// Verify non-decreasing. Strict-increasing holds at this operating
	// point (none of the shallow multiples hit the T_commit cap) but the
	// underlying obft.Config.Validate only requires non-decreasing post-
	// relaxation; the test asserts the relaxed invariant.
	for k := 1; k < len(got); k++ {
		require.GreaterOrEqualf(t, got[k], got[k-1],
			"budget must be non-decreasing in layer index; got[%d]=%v, got[%d]=%v",
			k, got[k], k-1, got[k-1])
	}
}

// TestDefaultBroadcastBudgetSchedule_K10_InterpolatesCleanly verifies the
// K=10 interpolation path (n=10 cluster).
func TestDefaultBroadcastBudgetSchedule_K10_InterpolatesCleanly(t *testing.T) {
	got, err := DefaultBroadcastBudgetSchedule(10, DefaultBTT, DefaultTCommit)
	require.NoError(t, err)
	require.Len(t, got, 10)
	require.Equal(t, 200*time.Millisecond, got[0], "L_0 must be 1·BTT")
	require.Equal(t, DefaultTCommit, got[9], "L_K-1 must be T_commit")

	// Verify non-decreasing (relaxed from strict-increasing in Phase 2).
	for k := 1; k < len(got); k++ {
		require.GreaterOrEqualf(t, got[k], got[k-1],
			"budget must be non-decreasing in layer index; got[%d]=%v, got[%d]=%v",
			k, got[k], k-1, got[k-1])
	}
}

// TestDefaultBroadcastBudgetSchedule_TCommitTooSmall_Caps verifies the
// helper caps shallow B_k at T_commit at degraded operating points
// (was an error pre-relaxation; now the helper returns a non-decreasing
// schedule with multiple layers clamping at BFT_start at runtime).
func TestDefaultBroadcastBudgetSchedule_TCommitTooSmall_Caps(t *testing.T) {
	// At BTT=400ms, T_commit=1000ms = 2.5·BTT. Pre-cap shallow values
	// would be [400, 600, 1000, 1000]; the cap leaves them unchanged
	// since out[2] = 2.5·BTT exactly equals T_commit. Pick an even
	// tighter T_commit to force a real cap.
	got, err := DefaultBroadcastBudgetSchedule(4, 400*time.Millisecond, 800*time.Millisecond)
	require.NoError(t, err)
	require.Len(t, got, 4)
	require.Equal(t, 400*time.Millisecond, got[0])
	require.Equal(t, 600*time.Millisecond, got[1])
	require.Equal(t, 800*time.Millisecond, got[2], "capped: was 2.5·BTT=1000ms, > T_commit=800ms")
	require.Equal(t, 800*time.Millisecond, got[3], "deepest = T_commit")
	for k := 1; k < len(got); k++ {
		require.GreaterOrEqualf(t, got[k], got[k-1], "non-decreasing post-cap")
	}
}

// TestDefaultBroadcastBudgetSchedule_EndpointConstantMatchK4 guards against
// drift between primaryBudgetDefaultBTT100 and the K=4 tabulated schedule's
// L_0 entry. The deepest endpoint is always T_commit (no constant to
// guard).
func TestDefaultBroadcastBudgetSchedule_EndpointConstantMatchK4(t *testing.T) {
	k4 := defaultLayerSchedules[4]
	require.Equal(t, primaryBudgetDefaultBTT100, k4.shallowBudgetBTT100[0],
		"primary budget endpoint must match defaultLayerSchedules[4][0]")
}

// TestDefaultTCommitDecomposition asserts the spec's §Application / Timing
// budget decomposition at Config A: the post-T_commit window of 600ms = 3 BTT
// splits as Δ_2 (400ms = 2 BTT) + Δ_3 (50ms = ε_3) + JitterBuffer (50ms) +
// TestConfigForCluster_NilOverrides — passing nil for the *ConfigOverrides
// arg must not panic; the function normalizes nil to a zero-valued struct
// so subsequent field reads (FetchAt, BroadcastBudget) are nil-safe. Spec
// referent: the accessor methods k()/btt()/delta2()/etc. are nil-safe,
// but the direct field reads on lines 340/348 of config.go aren't —
// hence the normalization at the top of ConfigForCluster.
func TestConfigForCluster_NilOverrides(t *testing.T) {
	committee := []spectypes.OperatorID{1, 2, 3, 4}
	cfg, err := ConfigForCluster(phase0.Slot(1), committee, [32]byte{0x01}, nil)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, DefaultK, cfg.K(), "nil overrides → DefaultK")
}

// TestConfigForCluster_KDerivedFromClusterSize — for n=10 (f=3) and
// n=13 (f=4) the BFT-liveness floor (K ≥ f+1) exceeds DefaultK=4.
// Production callers must set the K override; without it ConfigForCluster
// rejects. This test confirms the n=10/n=13 paths work when K is set
// correctly (validates the BroadcastBudget schedule's length-matches-K
// invariant at those K values).
func TestConfigForCluster_KDerivedFromClusterSize(t *testing.T) {
	cases := []struct {
		n int
		k int
	}{
		{4, 4},  // f=1, DefaultK
		{7, 4},  // f=2, K=f+2 (= 4)
		{10, 5}, // f=3, K=f+2 (= 5)
		{13, 6}, // f=4, K=f+2 (= 6)
	}
	for _, tc := range cases {
		tc := tc
		t.Run("n="+strconv.Itoa(tc.n)+"/K="+strconv.Itoa(tc.k), func(t *testing.T) {
			committee := make([]spectypes.OperatorID, tc.n)
			for i := range committee {
				committee[i] = spectypes.OperatorID(i + 1)
			}
			budget, err := DefaultBroadcastBudgetSchedule(tc.k, DefaultBTT, DefaultTCommit)
			require.NoError(t, err)
			overrides := &ConfigOverrides{
				K:               tc.k,
				BroadcastBudget: budget,
			}
			cfg, err := ConfigForCluster(phase0.Slot(1), committee, [32]byte{0x01}, overrides)
			require.NoError(t, err, "n=%d K=%d must validate", tc.n, tc.k)
			require.Equal(t, tc.k, cfg.K())
		})
	}
}

// HeaderSubmitHeadroom (100ms), summing to RelayCutoff − T_commit.
// Catches accidental drift between the spec's named components and the
// derived T_commit value.
func TestDefaultTCommitDecomposition(t *testing.T) {
	require.Equal(t, 400*time.Millisecond, DefaultDelta2, "Δ_2 = 2 BTT at default")
	require.Equal(t, 50*time.Millisecond, DefaultDelta3, "Δ_3 = ε_3 ≈ 50ms (spec)")
	require.Equal(t, 50*time.Millisecond, DefaultJitterBuffer, "JitterBuffer ≈ 50ms (spec)")
	require.Equal(t, 100*time.Millisecond, DefaultHeaderSubmitHeadroom)
	require.Equal(t, 3400*time.Millisecond, DefaultTCommit,
		"T_commit = RelayCutoff − HeaderSubmitHeadroom − JitterBuffer − Δ_3 − Δ_2")
	// Post-T_commit window sums to 600ms = 3 BTT exactly.
	post := DefaultRelayCutoff - DefaultTCommit
	require.Equal(t, 600*time.Millisecond, post)
	require.Equal(t, post, DefaultDelta2+DefaultDelta3+DefaultJitterBuffer+DefaultHeaderSubmitHeadroom)
}
