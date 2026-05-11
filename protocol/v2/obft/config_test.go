package obft

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func validBaseConfig() *Config {
	const btt = 150 * time.Millisecond // P99=100 + δ=50 fixture
	const tCommit = 1500 * time.Millisecond
	budgets := DefaultBroadcastBudget(4, btt)
	// Per-layer T_broadcast_max at this fixture: 1350 / 1275 / 1125 / 675ms.
	// FetchAt values chosen well below all caps to leave headroom for tests
	// that perturb FetchAt to trigger specific errors.
	return &Config{
		Height:    1,
		ClusterID: [32]byte{0x01},
		Operators: []OperatorID{1, 2, 3, 4},
		F:         1,
		Layers: []LayerSpec{
			{Leader: 1, FetchAt: 500 * time.Millisecond, BroadcastBudget: budgets[0]},
			{Leader: 2, FetchAt: 400 * time.Millisecond, BroadcastBudget: budgets[1]},
			{Leader: 3, FetchAt: 300 * time.Millisecond, BroadcastBudget: budgets[2]},
			{Leader: 4, FetchAt: 200 * time.Millisecond, BroadcastBudget: budgets[3]},
		},
		TCommit: tCommit,
		Delta2:  300 * time.Millisecond,
		Delta3:  250 * time.Millisecond,
		BTT:     btt,
	}
}

func TestConfig_Validate_OK(t *testing.T) {
	require.NoError(t, validBaseConfig().Validate())
}

func TestConfig_Validate_RejectsKTooSmall(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Layers = cfg.Layers[:2]
	require.ErrorContains(t, cfg.Validate(), "below late-leader-resilience minimum")
}

func TestConfig_Validate_RejectsClusterSizeTooSmall(t *testing.T) {
	cfg := validBaseConfig()
	// Drop one operator and trim layers to K=3 to pass the K-vs-cluster check
	// and isolate the 3F+1 check (F=1 requires cluster size >= 4).
	cfg.Operators = []OperatorID{1, 2, 3}
	cfg.Layers = cfg.Layers[:3]
	require.ErrorContains(t, cfg.Validate(), "3F+1")
}

func TestConfig_Validate_RejectsDuplicateLeader(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Layers[1].Leader = cfg.Layers[0].Leader
	require.ErrorContains(t, cfg.Validate(), "duplicate leader")
}

func TestConfig_Validate_RejectsNonMonotonicFetchAt(t *testing.T) {
	cfg := validBaseConfig()
	// Layer 1's FetchAt > layer 0's — violates T_{K-1} < ... < T_0.
	cfg.Layers[1].FetchAt = cfg.Layers[0].FetchAt + 100*time.Millisecond
	require.ErrorContains(t, cfg.Validate(), "strictly decreasing")
}

func TestConfig_Validate_RejectsFetchAtPastBroadcastDeadline(t *testing.T) {
	cfg := validBaseConfig()
	// L_0's T_broadcast_max = TCommit - B_0 = 1500 - 150 = 1350ms.
	cfg.Layers[0].FetchAt = 1400 * time.Millisecond
	require.ErrorContains(t, cfg.Validate(), "broadcast deadline")
}

func TestConfig_Validate_RejectsDelta2BelowBFTMin(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Delta2 = cfg.BTT - 1
	require.ErrorContains(t, cfg.Validate(), "Delta2")
}

func TestConfig_DerivedOffsets(t *testing.T) {
	cfg := validBaseConfig()
	// Per-layer broadcast-max offsets follow T_commit - B_k:
	for k := range cfg.Layers {
		require.Equal(t, cfg.TCommit-cfg.Layers[k].BroadcastBudget, cfg.BroadcastMaxOffsetForLayer(k))
	}
	require.Equal(t, cfg.TCommit, cfg.PhaseTwoStartOffset())
	require.Equal(t, cfg.TCommit+cfg.Delta2, cfg.PhaseTwoEndOffset())
	require.Equal(t, cfg.TCommit+cfg.Delta2+cfg.Delta3, cfg.RoundEndOffset())
	require.Equal(t, 4, cfg.K())
	require.Equal(t, 3, cfg.QV())
	require.Equal(t, 3, cfg.QEnc())
}

// ---- M3: per-layer staggered broadcast deadlines (BroadcastBudget) ----

// validStaggeredConfig returns a config with spec-recommended per-layer
// BroadcastBudget values (B_0=0.5 BTT, B_1=1 BTT, B_2=2 BTT, B_3=5 BTT)
// scaled to the test's BTT (D+δ = 150ms here).
func validStaggeredConfig() *Config {
	cfg := validBaseConfig()
	btt := cfg.BTT // 150ms
	// Adjust TCommit so the deepest layer's B_3 = 5*BTT fits.
	cfg.TCommit = 5*btt + 100*time.Millisecond
	cfg.Layers[0].BroadcastBudget = btt / 2 // B_0 = 0.5 BTT
	cfg.Layers[1].BroadcastBudget = btt     // B_1 = 1 BTT
	cfg.Layers[2].BroadcastBudget = 2 * btt // B_2 = 2 BTT
	cfg.Layers[3].BroadcastBudget = 5 * btt // B_3 = 5 BTT
	// FetchAt within each layer's per-layer cap T_commit - B_k.
	cfg.Layers[0].FetchAt = cfg.TCommit - cfg.Layers[0].BroadcastBudget
	cfg.Layers[1].FetchAt = cfg.TCommit - cfg.Layers[1].BroadcastBudget
	cfg.Layers[2].FetchAt = cfg.TCommit - cfg.Layers[2].BroadcastBudget
	cfg.Layers[3].FetchAt = cfg.TCommit - cfg.Layers[3].BroadcastBudget
	return cfg
}

func TestConfig_BroadcastBudget_PerLayerCap_OK(t *testing.T) {
	cfg := validStaggeredConfig()
	require.NoError(t, cfg.Validate())
	// Per-layer caps: deeper layers have earlier broadcast deadlines.
	require.Greater(t, cfg.BroadcastMaxOffsetForLayer(0), cfg.BroadcastMaxOffsetForLayer(1))
	require.Greater(t, cfg.BroadcastMaxOffsetForLayer(1), cfg.BroadcastMaxOffsetForLayer(2))
	require.Greater(t, cfg.BroadcastMaxOffsetForLayer(2), cfg.BroadcastMaxOffsetForLayer(3))
}

func TestConfig_BroadcastBudget_PrimaryAllowedTighter(t *testing.T) {
	// Spec: B_0 < BFT-min (2*BTT) is OK at the operating point — primary
	// trades reliability for MEV; deeper layers cover. Deepest layer must
	// still satisfy BFT-min.
	cfg := validStaggeredConfig()
	bftMin := 2 * cfg.BTT
	require.Less(t, cfg.Layers[0].BroadcastBudget, bftMin, "test setup: B_0 must be < BFT-min for this test to be meaningful")
	require.GreaterOrEqual(t, cfg.Layers[3].BroadcastBudget, bftMin)
	require.NoError(t, cfg.Validate())
}

func TestConfig_BroadcastBudget_RejectsNonMonotonic(t *testing.T) {
	cfg := validStaggeredConfig()
	// B_1 ≤ B_0 violates strict monotonicity.
	cfg.Layers[1].BroadcastBudget = cfg.Layers[0].BroadcastBudget
	require.ErrorContains(t, cfg.Validate(), "strictly increasing")
}

func TestConfig_BroadcastBudget_RejectsZeroOnAnyLayer(t *testing.T) {
	cfg := validStaggeredConfig()
	// Zero on any single layer is rejected — BroadcastBudget must be > 0
	// everywhere.
	cfg.Layers[2].BroadcastBudget = 0
	require.ErrorContains(t, cfg.Validate(), "must be > 0")
}

func TestConfig_BroadcastBudget_RejectsDeepestBelowBFTMin(t *testing.T) {
	cfg := validStaggeredConfig()
	bftMin := 2 * cfg.BTT
	// Set deepest below BFT-min — cluster has no liveness guarantee.
	cfg.Layers[3].BroadcastBudget = bftMin - 1
	// Bump shallower layers down to keep monotonicity intact.
	cfg.Layers[0].BroadcastBudget = bftMin / 8
	cfg.Layers[1].BroadcastBudget = bftMin / 4
	cfg.Layers[2].BroadcastBudget = bftMin / 2
	// Adjust FetchAt so per-layer caps remain feasible.
	for k := range cfg.Layers {
		cfg.Layers[k].FetchAt = cfg.TCommit - cfg.Layers[k].BroadcastBudget
	}
	require.ErrorContains(t, cfg.Validate(), "no liveness guarantee")
}

func TestConfig_BroadcastBudget_RejectsAllZero(t *testing.T) {
	// Per-layer BroadcastBudget is required on every layer; the historical
	// "all-zero fallback to a uniform 2·BTT cap" mode is no longer supported.
	// Callers should populate via DefaultBroadcastBudget(K, BTT) or an
	// explicit per-layer schedule.
	cfg := validBaseConfig()
	for k := range cfg.Layers {
		cfg.Layers[k].BroadcastBudget = 0
	}
	require.ErrorContains(t, cfg.Validate(), "must be > 0")
}

func TestConfig_BroadcastBudget_RejectsFetchAtExceedingPerLayerCap(t *testing.T) {
	cfg := validStaggeredConfig()
	// Push L_0 fetch past its (tight) per-layer cap. (L_1's FetchAt was
	// originally ≤ L_0's; bumping L_0 keeps it valid relative to L_1.)
	cfg.Layers[0].FetchAt = cfg.TCommit - cfg.Layers[0].BroadcastBudget + 1
	require.ErrorContains(t, cfg.Validate(), "exceeds broadcast deadline")
}

func TestConfig_BroadcastBudget_RejectsNegative(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Layers[1].BroadcastBudget = -1 * time.Millisecond
	require.ErrorContains(t, cfg.Validate(), "must be > 0")
}
