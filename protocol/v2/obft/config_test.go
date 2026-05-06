package obft

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func validBaseConfig() *Config {
	return &Config{
		Height:    1,
		ClusterID: [32]byte{0x01},
		Operators: []OperatorID{1, 2, 3, 4},
		F:         1,
		Layers: []LayerSpec{
			{Leader: 1, FetchAt: 1100 * time.Millisecond},
			{Leader: 2, FetchAt: 1050 * time.Millisecond},
			{Leader: 3, FetchAt: 1000 * time.Millisecond},
			{Leader: 4, FetchAt: 950 * time.Millisecond},
		},
		TCommit: 1500 * time.Millisecond,
		Delta2:  300 * time.Millisecond,
		Delta3:  250 * time.Millisecond,
		D:       100 * time.Millisecond,
		Delta:   50 * time.Millisecond,
	}
}

func TestConfig_Validate_OK(t *testing.T) {
	require.NoError(t, validBaseConfig().Validate())
}

func TestConfig_Validate_RejectsKTooSmall(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Layers = cfg.Layers[:2]
	require.ErrorContains(t, cfg.Validate(), "K must be >= 3")
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
	// Layer 1's FetchAt > layer 0's — violates T_{K-1} <= ... <= T_0.
	cfg.Layers[1].FetchAt = cfg.Layers[0].FetchAt + 100*time.Millisecond
	require.ErrorContains(t, cfg.Validate(), "non-increasing")
}

func TestConfig_Validate_RejectsFetchAtPastBroadcastDeadline(t *testing.T) {
	cfg := validBaseConfig()
	// T_broadcast_max = TCommit - 2*(D+δ) = 1500 - 300 = 1200ms.
	cfg.Layers[0].FetchAt = 1300 * time.Millisecond
	require.ErrorContains(t, cfg.Validate(), "broadcast deadline")
}

func TestConfig_Validate_RejectsDelta2BelowBFTMin(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Delta2 = cfg.D + cfg.Delta - 1
	require.ErrorContains(t, cfg.Validate(), "Delta2")
}

func TestConfig_DerivedOffsets(t *testing.T) {
	cfg := validBaseConfig()
	require.Equal(t, cfg.TCommit-2*(cfg.D+cfg.Delta), cfg.BroadcastMaxOffset())
	require.Equal(t, cfg.TCommit, cfg.PhaseTwoStartOffset())
	require.Equal(t, cfg.TCommit+cfg.Delta2, cfg.PhaseTwoEndOffset())
	require.Equal(t, cfg.TCommit+cfg.Delta2+cfg.Delta3, cfg.RoundEndOffset())
	require.Equal(t, 4, cfg.K())
	require.Equal(t, 3, cfg.QV())
	require.Equal(t, 3, cfg.QEnc())
}
