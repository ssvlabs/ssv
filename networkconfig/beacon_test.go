package networkconfig

import (
	"math"
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
)

// TestForkAtEpoch verifies that ForkAtEpoch returns the correct version and fork data based on fork epochs.
func TestForkAtEpoch(t *testing.T) {
	config := &Beacon{
		Forks: map[spec.DataVersion]phase0.Fork{
			spec.DataVersionPhase0: {
				Epoch:           phase0.Epoch(0),
				PreviousVersion: phase0.Version{0},
				CurrentVersion:  phase0.Version{0},
			},
			spec.DataVersionAltair: {
				Epoch:           phase0.Epoch(10),
				PreviousVersion: phase0.Version{0},
				CurrentVersion:  phase0.Version{1},
			},
			spec.DataVersionBellatrix: {
				Epoch:           phase0.Epoch(20),
				PreviousVersion: phase0.Version{1},
				CurrentVersion:  phase0.Version{2},
			},
			spec.DataVersionCapella: {
				Epoch:           phase0.Epoch(30),
				PreviousVersion: phase0.Version{2},
				CurrentVersion:  phase0.Version{3},
			},
			spec.DataVersionDeneb: {
				Epoch:           phase0.Epoch(40),
				PreviousVersion: phase0.Version{3},
				CurrentVersion:  phase0.Version{4},
			},
			spec.DataVersionElectra: {
				Epoch:           phase0.Epoch(50),
				PreviousVersion: phase0.Version{4},
				CurrentVersion:  phase0.Version{5},
			},
			spec.DataVersionFulu: {
				Epoch:           phase0.Epoch(60),
				PreviousVersion: phase0.Version{5},
				CurrentVersion:  phase0.Version{6},
			},
		},
	}

	tests := []struct {
		epoch   phase0.Epoch
		version spec.DataVersion
		fork    phase0.Fork
	}{
		{epoch: 0, version: spec.DataVersionPhase0, fork: phase0.Fork{
			PreviousVersion: phase0.Version{0},
			CurrentVersion:  phase0.Version{0},
			Epoch:           0,
		}},
		{epoch: 9, version: spec.DataVersionPhase0, fork: phase0.Fork{
			PreviousVersion: phase0.Version{0},
			CurrentVersion:  phase0.Version{0},
			Epoch:           0,
		}},
		{epoch: 10, version: spec.DataVersionAltair, fork: phase0.Fork{
			Epoch:           phase0.Epoch(10),
			PreviousVersion: phase0.Version{0},
			CurrentVersion:  phase0.Version{1},
		}},
		{epoch: 15, version: spec.DataVersionAltair, fork: phase0.Fork{
			Epoch:           phase0.Epoch(10),
			PreviousVersion: phase0.Version{0},
			CurrentVersion:  phase0.Version{1},
		}},
		{epoch: 20, version: spec.DataVersionBellatrix, fork: phase0.Fork{
			Epoch:           phase0.Epoch(20),
			PreviousVersion: phase0.Version{1},
			CurrentVersion:  phase0.Version{2},
		}},
		{epoch: 25, version: spec.DataVersionBellatrix, fork: phase0.Fork{
			Epoch:           phase0.Epoch(20),
			PreviousVersion: phase0.Version{1},
			CurrentVersion:  phase0.Version{2},
		}},
		{epoch: 30, version: spec.DataVersionCapella, fork: phase0.Fork{
			Epoch:           phase0.Epoch(30),
			PreviousVersion: phase0.Version{2},
			CurrentVersion:  phase0.Version{3},
		}},
		{epoch: 35, version: spec.DataVersionCapella, fork: phase0.Fork{
			Epoch:           phase0.Epoch(30),
			PreviousVersion: phase0.Version{2},
			CurrentVersion:  phase0.Version{3},
		}},
		{epoch: 40, version: spec.DataVersionDeneb, fork: phase0.Fork{
			Epoch:           phase0.Epoch(40),
			PreviousVersion: phase0.Version{3},
			CurrentVersion:  phase0.Version{4},
		}},
		{epoch: 45, version: spec.DataVersionDeneb, fork: phase0.Fork{
			Epoch:           phase0.Epoch(40),
			PreviousVersion: phase0.Version{3},
			CurrentVersion:  phase0.Version{4},
		}},
		{epoch: 50, version: spec.DataVersionElectra, fork: phase0.Fork{
			Epoch:           phase0.Epoch(50),
			PreviousVersion: phase0.Version{4},
			CurrentVersion:  phase0.Version{5},
		}},
		{epoch: 55, version: spec.DataVersionElectra, fork: phase0.Fork{
			Epoch:           phase0.Epoch(50),
			PreviousVersion: phase0.Version{4},
			CurrentVersion:  phase0.Version{5},
		}},
		{epoch: 60, version: spec.DataVersionFulu, fork: phase0.Fork{
			Epoch:           phase0.Epoch(60),
			PreviousVersion: phase0.Version{5},
			CurrentVersion:  phase0.Version{6},
		}},
		{epoch: 65, version: spec.DataVersionFulu, fork: phase0.Fork{
			Epoch:           phase0.Epoch(60),
			PreviousVersion: phase0.Version{5},
			CurrentVersion:  phase0.Version{6},
		}},
	}

	for _, tc := range tests {
		version, fork := config.ForkAtEpoch(tc.epoch)
		require.Equal(t, tc.version, version, "Wrong version")
		require.NotNil(t, tc.fork, fork, "Nil fork")
		require.Equal(t, tc.fork, *fork, "Wrong fork")
	}
}

// TestForkAtEpochGloas verifies that a scheduled Gloas fork is returned from its epoch on, while an
// unscheduled (far-future) or absent Gloas entry keeps Fulu as the latest fork.
func TestForkAtEpochGloas(t *testing.T) {
	fuluFork := phase0.Fork{Epoch: 60, PreviousVersion: phase0.Version{5}, CurrentVersion: phase0.Version{6}}
	gloasFork := phase0.Fork{Epoch: 70, PreviousVersion: phase0.Version{6}, CurrentVersion: phase0.Version{7}}
	newConfig := func(gloas *phase0.Fork) *Beacon {
		forks := map[spec.DataVersion]phase0.Fork{
			spec.DataVersionPhase0:    {Epoch: 0},
			spec.DataVersionAltair:    {Epoch: 10, CurrentVersion: phase0.Version{1}},
			spec.DataVersionBellatrix: {Epoch: 20, PreviousVersion: phase0.Version{1}, CurrentVersion: phase0.Version{2}},
			spec.DataVersionCapella:   {Epoch: 30, PreviousVersion: phase0.Version{2}, CurrentVersion: phase0.Version{3}},
			spec.DataVersionDeneb:     {Epoch: 40, PreviousVersion: phase0.Version{3}, CurrentVersion: phase0.Version{4}},
			spec.DataVersionElectra:   {Epoch: 50, PreviousVersion: phase0.Version{4}, CurrentVersion: phase0.Version{5}},
			spec.DataVersionFulu:      fuluFork,
		}
		if gloas != nil {
			forks[DataVersionGloas] = *gloas
		}
		return &Beacon{Forks: forks}
	}

	t.Run("scheduled", func(t *testing.T) {
		config := newConfig(&gloasFork)

		version, fork := config.ForkAtEpoch(69)
		require.Equal(t, spec.DataVersionFulu, version)
		require.Equal(t, fuluFork, *fork)

		version, fork = config.ForkAtEpoch(70)
		require.Equal(t, DataVersionGloas, version)
		require.Equal(t, gloasFork, *fork)

		version, fork = config.ForkAtEpoch(1_000_000)
		require.Equal(t, DataVersionGloas, version)
		require.Equal(t, gloasFork, *fork)
	})

	t.Run("far future", func(t *testing.T) {
		farFuture := gloasFork
		farFuture.Epoch = math.MaxUint64
		config := newConfig(&farFuture)

		version, fork := config.ForkAtEpoch(1_000_000)
		require.Equal(t, spec.DataVersionFulu, version)
		require.Equal(t, fuluFork, *fork)
	})

	t.Run("absent", func(t *testing.T) {
		config := newConfig(nil)

		version, fork := config.ForkAtEpoch(1_000_000)
		require.Equal(t, spec.DataVersionFulu, version)
		require.Equal(t, fuluFork, *fork)
	})
}

func TestSyncCommitteePeriodHelpers(t *testing.T) {
	config := &Beacon{
		SlotsPerEpoch:                32,
		EpochsPerSyncCommitteePeriod: 8, // 8 * 32 = 256 slots per period
	}

	tests := []struct {
		name               string
		period             uint64
		firstEpoch         phase0.Epoch
		lastActionableSlot phase0.Slot
		firstNextSlot      phase0.Slot
	}{
		{
			name:               "first period",
			period:             0,
			firstEpoch:         0,
			firstNextSlot:      256,
			lastActionableSlot: 254,
		},
		{
			name:               "second period",
			period:             1,
			firstEpoch:         8,
			firstNextSlot:      512,
			lastActionableSlot: 510,
		},
		{
			name:               "third period",
			period:             2,
			firstEpoch:         16,
			firstNextSlot:      768,
			lastActionableSlot: 766,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.firstEpoch, config.FirstEpochOfSyncPeriod(tc.period))
			require.Equal(t, tc.firstNextSlot, config.FirstSlotAtEpoch(config.FirstEpochOfSyncPeriod(tc.period+1)))
			require.Equal(t, tc.lastActionableSlot, config.LastActionableSlotOfSyncPeriod(tc.period))
			require.Equal(t, tc.firstNextSlot-2, config.LastActionableSlotOfSyncPeriod(tc.period))
		})
	}
}
