package obft

import (
	"strconv"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

// TestDefaultBroadcastBudgetSchedule_K3_AtConfigA verifies the K=3 default
// with SafetyBuffer=DefaultSafetyBuffer (700ms): only L_0 has a tighter
// budget (2·BTT + SafetyBuffer = 1100ms); backups all = T_commit.
func TestDefaultBroadcastBudgetSchedule_K3_AtConfigA(t *testing.T) {
	got, err := DefaultBroadcastBudgetSchedule(3, DefaultBTT, DefaultSafetyBuffer, DefaultTCommit)
	require.NoError(t, err)
	want := []time.Duration{
		1100 * time.Millisecond, // 2·BTT + SafetyBuffer = 400 + 700
		DefaultTCommit,          // L_1 = T_commit (backup broadcasts at BFT_start)
		DefaultTCommit,          // L_2 = T_commit (backup broadcasts at BFT_start)
	}
	require.Equal(t, want, got)
}

// TestDefaultBroadcastBudgetSchedule_K4_AtConfigA verifies the K=4 up-tier
// schedule with SafetyBuffer=DefaultSafetyBuffer (700ms). L_0 = 2·BTT +
// SafetyBuffer; backups L_1..L_3 all = T_commit. K=4 is the up-tier
// deployment config (deeper fall-through); the post-flip default is K=2
// (= f+1 BFT-min). This test exercises the K=4 schedule shape.
func TestDefaultBroadcastBudgetSchedule_K4_AtConfigA(t *testing.T) {
	got, err := DefaultBroadcastBudgetSchedule(4, DefaultBTT, DefaultSafetyBuffer, DefaultTCommit)
	require.NoError(t, err)
	want := []time.Duration{
		1100 * time.Millisecond, // 2·BTT + SafetyBuffer
		DefaultTCommit,
		DefaultTCommit,
		DefaultTCommit,
	}
	require.Equal(t, want, got)
}

// TestDefaultBroadcastBudgetSchedule_K4_NoSafetyBuffer verifies the
// fully-meshed-cluster opt-out: at SafetyBuffer=0 the primary's budget
// collapses to 2·BTT; backups unchanged at T_commit.
func TestDefaultBroadcastBudgetSchedule_K4_NoSafetyBuffer(t *testing.T) {
	got, err := DefaultBroadcastBudgetSchedule(4, DefaultBTT, 0, DefaultTCommit)
	require.NoError(t, err)
	want := []time.Duration{
		400 * time.Millisecond, // 2·BTT
		DefaultTCommit,
		DefaultTCommit,
		DefaultTCommit,
	}
	require.Equal(t, want, got)
}

// TestDefaultBroadcastBudgetSchedule_K4_ScalesWithBTT verifies the primary
// budget's BTT-multiplier portion scales linearly when BTT changes — at
// BTT=400ms with SafetyBuffer=0 the primary becomes 800ms; backups stay
// anchored at T_commit.
func TestDefaultBroadcastBudgetSchedule_K4_ScalesWithBTT(t *testing.T) {
	got, err := DefaultBroadcastBudgetSchedule(4, 400*time.Millisecond, 0, DefaultTCommit)
	require.NoError(t, err)
	want := []time.Duration{
		800 * time.Millisecond,
		DefaultTCommit,
		DefaultTCommit,
		DefaultTCommit,
	}
	require.Equal(t, want, got)
}

// TestDefaultBroadcastBudgetSchedule_K7 verifies the K=7 default (n=7
// cluster): same shape as K=4 — only L_0 has a tighter budget; all backups
// = T_commit.
func TestDefaultBroadcastBudgetSchedule_K7(t *testing.T) {
	got, err := DefaultBroadcastBudgetSchedule(7, DefaultBTT, DefaultSafetyBuffer, DefaultTCommit)
	require.NoError(t, err)
	require.Len(t, got, 7)
	require.Equal(t, 1100*time.Millisecond, got[0], "L_0 = 2·BTT + SafetyBuffer")
	for k := 1; k < len(got); k++ {
		require.Equal(t, DefaultTCommit, got[k],
			"L_%d must be T_commit (backup broadcasts at BFT_start)", k)
	}
}

// TestDefaultBroadcastBudgetSchedule_K10 verifies the K=10 default path
// (n=10 cluster).
func TestDefaultBroadcastBudgetSchedule_K10(t *testing.T) {
	got, err := DefaultBroadcastBudgetSchedule(10, DefaultBTT, DefaultSafetyBuffer, DefaultTCommit)
	require.NoError(t, err)
	require.Len(t, got, 10)
	require.Equal(t, 1100*time.Millisecond, got[0], "L_0 = 2·BTT + SafetyBuffer")
	for k := 1; k < len(got); k++ {
		require.Equal(t, DefaultTCommit, got[k],
			"L_%d must be T_commit (backup broadcasts at BFT_start)", k)
	}
}

// TestDefaultBroadcastBudgetSchedule_TCommitTooSmall_Caps verifies the
// helper caps the primary's B_0 at T_commit at degraded operating points
// (otherwise B_0 would exceed T_commit, leaving the primary unable to
// even broadcast at BFT_start). Uses SafetyBuffer=0 so the cap behavior is
// BTT-driven rather than SafetyBuffer-driven.
func TestDefaultBroadcastBudgetSchedule_TCommitTooSmall_Caps(t *testing.T) {
	// At BTT=600ms, T_commit=1000ms: B_0 pre-cap = 1200ms; caps to 1000ms.
	got, err := DefaultBroadcastBudgetSchedule(4, 600*time.Millisecond, 0, 1000*time.Millisecond)
	require.NoError(t, err)
	require.Len(t, got, 4)
	require.Equal(t, 1000*time.Millisecond, got[0], "primary capped: was 2·BTT=1200ms, > T_commit=1000ms")
	require.Equal(t, 1000*time.Millisecond, got[1])
	require.Equal(t, 1000*time.Millisecond, got[2])
	require.Equal(t, 1000*time.Millisecond, got[3], "deepest = T_commit")
	for k := 1; k < len(got); k++ {
		require.GreaterOrEqualf(t, got[k], got[k-1], "non-decreasing post-cap")
	}
}

// TestDefaultBroadcastBudgetSchedule_NegativeSafetyBuffer_Rejected guards
// against accidental negative SafetyBuffer values producing nonsensical
// schedules.
func TestDefaultBroadcastBudgetSchedule_NegativeSafetyBuffer_Rejected(t *testing.T) {
	_, err := DefaultBroadcastBudgetSchedule(4, DefaultBTT, -1*time.Millisecond, DefaultTCommit)
	require.ErrorContains(t, err, "SafetyBuffer")
}

// TestDefaultTCommitDecomposition asserts the spec's §Application / Timing
// budget decomposition at Config A: the post-T_commit window of 400ms = 2 BTT
// splits as Δ_2 (200ms = 1 BTT) + ε_3 (50ms) + JitterBuffer (50ms) +
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

// TestConfigForCluster_KDerivedFromClusterSize — for n ≥ 7 (f ≥ 2) the
// BFT-liveness floor (K ≥ f+1) exceeds DefaultK=2. Production callers must
// set the K override; without it ConfigForCluster rejects. This test
// confirms the BFT-min paths work when K is set correctly at each cluster
// size (validates the BroadcastBudget schedule's length-matches-K
// invariant at the K=f+1 default values).
func TestConfigForCluster_KDerivedFromClusterSize(t *testing.T) {
	cases := []struct {
		n int
		k int
	}{
		{4, 2},  // f=1, K=f+1 default (= DefaultK)
		{7, 3},  // f=2, K=f+1 BFT-min
		{10, 4}, // f=3, K=f+1 BFT-min
		{13, 5}, // f=4, K=f+1 BFT-min
	}
	for _, tc := range cases {
		tc := tc
		t.Run("n="+strconv.Itoa(tc.n)+"/K="+strconv.Itoa(tc.k), func(t *testing.T) {
			committee := make([]spectypes.OperatorID, tc.n)
			for i := range committee {
				committee[i] = spectypes.OperatorID(i + 1)
			}
			budget, err := DefaultBroadcastBudgetSchedule(tc.k, DefaultBTT, DefaultSafetyBuffer, DefaultTCommit)
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

// TestConfigForCluster_RecoveryFloor confirms a production-built Config carries
// the max(SafetyBuffer, 1·BTT) recovery floor on L_0's broadcast target — the
// end-to-end production lock (obft/base.BroadcastTargetOffset's math is unit-
// tested there; this pins that ConfigForCluster's tCommit/B_0/BTT wiring
// produces the floored deadline the runner broadcasts by). Mirrors 2abOBFT,
// which carries its max(SafetyBuffer, 1·BTT) floor in resolveBudget.
func TestConfigForCluster_RecoveryFloor(t *testing.T) {
	committee := []spectypes.OperatorID{1, 2, 3, 4} // n=4 → K=f+1=2=DefaultK

	// Default SafetyBuffer (700ms): B_0 = 2·BTT + 700 = 1100ms ≥ 3·BTT, so the
	// floor is dormant and the L_0 broadcast target equals the B_k-budget
	// deadline T_commit − B_0.
	cfgDefault, err := ConfigForCluster(phase0.Slot(1), committee, [32]byte{0x01}, nil)
	require.NoError(t, err)
	require.Equal(t, cfgDefault.BroadcastMaxOffsetForLayer(0), cfgDefault.BroadcastTargetForLayer(0),
		"floor dormant at default SafetyBuffer (B_0 ≥ 3·BTT): target = budget deadline")

	// SafetyBuffer≈0 opt-out (1ns; Go's zero-means-default forbids a literal 0
	// — see ConfigOverrides.SafetyBuffer): B_0 = 2·BTT < 3·BTT, so the floor
	// binds and the production L_0 broadcast target is pulled to T_commit −
	// 3·BTT, 1·BTT earlier than the B_k-budget deadline.
	cfgOptOut, err := ConfigForCluster(phase0.Slot(1), committee, [32]byte{0x01},
		&ConfigOverrides{SafetyBuffer: time.Nanosecond})
	require.NoError(t, err)
	require.Equal(t, cfgOptOut.TCommit-3*cfgOptOut.BTT, cfgOptOut.BroadcastTargetForLayer(0),
		"floor active at SafetyBuffer≈0: L_0 broadcast target = T_commit − 3·BTT")
	require.Less(t, cfgOptOut.BroadcastTargetForLayer(0), cfgOptOut.BroadcastMaxOffsetForLayer(0),
		"floored target is 1·BTT earlier than the B_k-budget deadline at the opt-out")
}

// HeaderSubmitHeadroom (100ms), summing to RelayCutoff − T_commit.
// Catches accidental drift between the spec's named components and the
// derived T_commit value.
func TestDefaultTCommitDecomposition(t *testing.T) {
	require.Equal(t, 200*time.Millisecond, DefaultDelta2, "Δ_2 = 1 BTT recommended (reflood lives in B_k)")
	require.Equal(t, 50*time.Millisecond, DefaultEps3, "ε_3 ≈ 50ms (spec)")
	require.Equal(t, 50*time.Millisecond, DefaultJitterBuffer, "JitterBuffer ≈ 50ms (spec)")
	require.Equal(t, 100*time.Millisecond, DefaultHeaderSubmitHeadroom)
	require.Equal(t, 3600*time.Millisecond, DefaultTCommit,
		"T_commit = RelayCutoff − HeaderSubmitHeadroom − JitterBuffer − ε_3 − Δ_2")
	// Post-T_commit window sums to 400ms = 2 BTT exactly.
	post := DefaultRelayCutoff - DefaultTCommit
	require.Equal(t, 400*time.Millisecond, post)
	require.Equal(t, post, DefaultDelta2+DefaultEps3+DefaultJitterBuffer+DefaultHeaderSubmitHeadroom)
}
