package networkconfig

import (
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
