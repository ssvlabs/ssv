package beaconcfg

import (
	"math"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
)

func TestConfigSlotAndEpochMath(t *testing.T) {
	genesis := time.Unix(1577836800, 0)
	cfg := &Config{
		SlotDuration:  12 * time.Second,
		SlotsPerEpoch: 32,
		GenesisTime:   genesis,
	}

	require.Equal(t, phase0.Slot(0), cfg.EstimatedSlotAtTime(genesis))
	require.Equal(t, phase0.Slot(2), cfg.EstimatedSlotAtTime(genesis.Add(24*time.Second)))
	require.Equal(t, phase0.Epoch(2), cfg.EstimatedEpochAtSlot(64))
	require.Equal(t, phase0.Slot(96), cfg.FirstSlotAtEpoch(3))
	require.Equal(t, 384*time.Second, cfg.EpochDuration())

	require.Panics(t, func() {
		_ = cfg.EstimatedSlotAtTime(genesis.Add(-time.Second))
	})
}

func TestConfigForkAtEpoch(t *testing.T) {
	cfg := &Config{
		Forks: map[spec.DataVersion]phase0.Fork{
			spec.DataVersionPhase0: {
				Epoch: 0,
			},
			spec.DataVersionAltair: {
				Epoch: 10,
			},
			spec.DataVersionBellatrix: {
				Epoch: 20,
			},
			spec.DataVersionCapella: {
				Epoch: 30,
			},
			spec.DataVersionDeneb: {
				Epoch: 40,
			},
			spec.DataVersionElectra: {
				Epoch: 50,
			},
			spec.DataVersionFulu: {
				Epoch: 60,
			},
		},
	}

	version, _ := cfg.ForkAtEpoch(0)
	require.Equal(t, spec.DataVersionPhase0, version)

	version, _ = cfg.ForkAtEpoch(10)
	require.Equal(t, spec.DataVersionAltair, version)

	version, _ = cfg.ForkAtEpoch(59)
	require.Equal(t, spec.DataVersionElectra, version)

	version, _ = cfg.ForkAtEpoch(80)
	require.Equal(t, spec.DataVersionFulu, version)
}

func TestConfigForkAtEpochGloas(t *testing.T) {
	cfg := &Config{
		Forks: map[spec.DataVersion]phase0.Fork{
			spec.DataVersionPhase0:  {Epoch: 0},
			spec.DataVersionAltair:  {Epoch: 10},
			spec.DataVersionElectra: {Epoch: 50},
			spec.DataVersionFulu:    {Epoch: 60, CurrentVersion: phase0.Version{6}},
			spec.DataVersionGloas:   {Epoch: 70, PreviousVersion: phase0.Version{6}, CurrentVersion: phase0.Version{7}},
		},
	}

	version, fork := cfg.ForkAtEpoch(69)
	require.Equal(t, spec.DataVersionFulu, version)
	require.Equal(t, phase0.Version{6}, fork.CurrentVersion)

	version, fork = cfg.ForkAtEpoch(70)
	require.Equal(t, spec.DataVersionGloas, version)
	require.Equal(t, phase0.Version{7}, fork.CurrentVersion)

	version, _ = cfg.ForkAtEpoch(1_000_000)
	require.Equal(t, spec.DataVersionGloas, version)

	// An unscheduled (far-future) Gloas fork keeps Fulu as the latest fork.
	cfg.Forks[spec.DataVersionGloas] = phase0.Fork{Epoch: math.MaxUint64}
	version, _ = cfg.ForkAtEpoch(1_000_000)
	require.Equal(t, spec.DataVersionFulu, version)
}

func TestConfigForkAtEpochMissingForkEntries(t *testing.T) {
	cfg := &Config{
		Forks: map[spec.DataVersion]phase0.Fork{
			spec.DataVersionPhase0: {
				Epoch: 0,
			},
			spec.DataVersionAltair: {
				Epoch: 10,
			},
			// Bellatrix intentionally missing.
			spec.DataVersionCapella: {
				Epoch: 30,
			},
			spec.DataVersionElectra: {
				Epoch: 50,
			},
		},
	}

	version, _ := cfg.ForkAtEpoch(25)
	require.Equal(t, spec.DataVersionAltair, version)

	version, _ = cfg.ForkAtEpoch(35)
	require.Equal(t, spec.DataVersionCapella, version)

	version, _ = cfg.ForkAtEpoch(80)
	require.Equal(t, spec.DataVersionElectra, version)
}
