package tbft

import (
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

func TestConfigForCluster_n4_IsTBFT2(t *testing.T) {
	cfg, err := ConfigForCluster(
		phase0.Slot(100),
		[]spectypes.OperatorID{1, 2, 3, 4},
		[32]byte{0xAB},
		nil,
	)
	require.NoError(t, err)
	require.True(t, IsTBFT2(cfg), "n=4 must produce TBFT2 (K=2)")
	require.Equal(t, 2, cfg.K())
	require.Equal(t, 1, cfg.F)
	require.Equal(t, 3, cfg.Quorum())

	// Layer 0 (primary) should be at LATE fetch; Layer 1 (backup) at EARLY fetch.
	require.Equal(t, DefaultLateFetchOffset, cfg.Layers[0].FetchAt,
		"TBFT2 primary leader fetches late")
	require.Equal(t, DefaultEarlyFetchOffset, cfg.Layers[1].FetchAt,
		"TBFT2 backup leader fetches early")
}

func TestConfigForCluster_n7_IsTBFT_K3(t *testing.T) {
	cfg, err := ConfigForCluster(
		phase0.Slot(100),
		[]spectypes.OperatorID{1, 2, 3, 4, 5, 6, 7},
		[32]byte{},
		nil,
	)
	require.NoError(t, err)
	require.False(t, IsTBFT2(cfg))
	require.Equal(t, 3, cfg.K(), "n=7: K = max(3, f+1) = max(3, 3) = 3")
	require.Equal(t, 2, cfg.F)

	// All layers fetch at the same (late) time for TBFT.
	for i, layer := range cfg.Layers {
		require.Equal(t, DefaultLateFetchOffset, layer.FetchAt,
			"TBFT layer %d should fetch late", i)
	}
}

func TestConfigForCluster_n10_IsTBFT_K4(t *testing.T) {
	cfg, err := ConfigForCluster(
		phase0.Slot(0),
		[]spectypes.OperatorID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		[32]byte{},
		nil,
	)
	require.NoError(t, err)
	require.Equal(t, 4, cfg.K(), "n=10: K = max(3, f+1) = max(3, 4) = 4")
	require.Equal(t, 3, cfg.F)
}

func TestConfigForCluster_n13_IsTBFT_K5(t *testing.T) {
	committee := make([]spectypes.OperatorID, 13)
	for i := 0; i < 13; i++ {
		committee[i] = spectypes.OperatorID(i + 1)
	}
	cfg, err := ConfigForCluster(phase0.Slot(0), committee, [32]byte{}, nil)
	require.NoError(t, err)
	require.Equal(t, 5, cfg.K(), "n=13: K = max(3, f+1) = max(3, 5) = 5")
	require.Equal(t, 4, cfg.F)
}

func TestConfigForCluster_LeaderRotation(t *testing.T) {
	// Same committee, different slots → leader rotation moves predictably.
	committee := []spectypes.OperatorID{1, 2, 3, 4, 5, 6, 7}

	cfg100, err := ConfigForCluster(100, committee, [32]byte{}, nil)
	require.NoError(t, err)
	cfg101, err := ConfigForCluster(101, committee, [32]byte{}, nil)
	require.NoError(t, err)

	// At slot 100: layer 0 leader index = 100 mod 7 = 2 → operator 3.
	require.Equal(t, uint64(3), uint64(cfg100.Layers[0].Leader))
	// At slot 101: layer 0 leader index = 101 mod 7 = 3 → operator 4.
	require.Equal(t, uint64(4), uint64(cfg101.Layers[0].Leader))

	// Layer 1 leader at slot 100 = (100+1) mod 7 = 3 → operator 4.
	require.Equal(t, uint64(4), uint64(cfg100.Layers[1].Leader))
}

func TestConfigForCluster_LeadersAreDistinct(t *testing.T) {
	committee := []spectypes.OperatorID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}
	cfg, err := ConfigForCluster(phase0.Slot(99), committee, [32]byte{}, nil)
	require.NoError(t, err)

	seen := make(map[uint64]bool)
	for i, layer := range cfg.Layers {
		require.False(t, seen[uint64(layer.Leader)],
			"layer %d leader (op %d) is a duplicate", i, layer.Leader)
		seen[uint64(layer.Leader)] = true
	}
}

func TestConfigForCluster_ProducedConfigValidates(t *testing.T) {
	// Spot-check that ConfigForCluster's output passes Config.Validate
	// for all SSV cluster sizes.
	for _, n := range []int{4, 7, 10, 13} {
		t.Run("", func(t *testing.T) {
			committee := make([]spectypes.OperatorID, n)
			for i := 0; i < n; i++ {
				committee[i] = spectypes.OperatorID(i + 1)
			}
			cfg, err := ConfigForCluster(phase0.Slot(42), committee, [32]byte{}, nil)
			require.NoError(t, err)
			require.NoError(t, cfg.Validate(), "n=%d config should validate", n)
		})
	}
}

func TestConfigForCluster_UnsortedCommitteeOK(t *testing.T) {
	// Even if caller passes an unsorted committee, the factory should sort
	// internally so the rotation is deterministic.
	committee := []spectypes.OperatorID{7, 5, 3, 1, 6, 2, 4}
	cfg1, err := ConfigForCluster(phase0.Slot(50), committee, [32]byte{}, nil)
	require.NoError(t, err)

	// Same operators in sorted order produce the same config.
	cfg2, err := ConfigForCluster(phase0.Slot(50),
		[]spectypes.OperatorID{1, 2, 3, 4, 5, 6, 7}, [32]byte{}, nil)
	require.NoError(t, err)

	require.Equal(t, cfg1.Operators, cfg2.Operators)
	for i := range cfg1.Layers {
		require.Equal(t, cfg1.Layers[i].Leader, cfg2.Layers[i].Leader,
			"layer %d leader differs after sorting", i)
	}
}

func TestConfigForCluster_ClusterSizeNotMultipleOf3fPlus1(t *testing.T) {
	tests := []int{2, 3, 5, 6, 8, 9, 11, 12}
	for _, n := range tests {
		committee := make([]spectypes.OperatorID, n)
		for i := 0; i < n; i++ {
			committee[i] = spectypes.OperatorID(i + 1)
		}
		_, err := ConfigForCluster(0, committee, [32]byte{}, nil)
		require.Error(t, err, "n=%d should reject (not 3f+1)", n)
	}
}

func TestConfigForCluster_EmptyCommittee(t *testing.T) {
	_, err := ConfigForCluster(0, nil, [32]byte{}, nil)
	require.ErrorContains(t, err, "empty committee")
}

func TestConfigForCluster_OverridesApplied(t *testing.T) {
	overrides := &ConfigOverrides{
		DeadlineOffset:   5 * time.Second,
		LateFetchOffset:  3 * time.Second,
		EarlyFetchOffset: -10 * time.Second,
	}
	committee := []spectypes.OperatorID{1, 2, 3, 4}
	cfg, err := ConfigForCluster(phase0.Slot(0), committee, [32]byte{}, overrides)
	require.NoError(t, err)
	require.Equal(t, 5*time.Second, cfg.Deadline)
	require.Equal(t, 3*time.Second, cfg.Layers[0].FetchAt)   // primary
	require.Equal(t, -10*time.Second, cfg.Layers[1].FetchAt) // backup early
}

func TestConfigForCluster_ClusterIDPropagated(t *testing.T) {
	id := [32]byte{0x01, 0x02, 0x03, 0x04, 0xAA, 0xBB, 0xCC, 0xDD}
	cfg, err := ConfigForCluster(0, []spectypes.OperatorID{1, 2, 3, 4}, id, nil)
	require.NoError(t, err)
	require.Equal(t, id, cfg.ClusterID)
}
