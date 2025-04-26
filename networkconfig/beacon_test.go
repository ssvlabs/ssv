package networkconfig

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// TestDataVersion verifies that DataVersion returns the correct version based on fork epochs.
func TestDataVersion(t *testing.T) {
	config := &BeaconConfig{
		Forks: map[spec.DataVersion]*phase0.Fork{
			spec.DataVersionPhase0: {
				Epoch: phase0.Epoch(0),
			},
			spec.DataVersionAltair: {
				Epoch: phase0.Epoch(10),
			},
			spec.DataVersionBellatrix: {
				Epoch: phase0.Epoch(20),
			},
			spec.DataVersionCapella: {
				Epoch: phase0.Epoch(30),
			},
			spec.DataVersionDeneb: {
				Epoch: phase0.Epoch(40),
			},
			spec.DataVersionElectra: {
				Epoch: phase0.Epoch(50),
			},
		},
	}

	tests := []struct {
		epoch    phase0.Epoch
		expected spec.DataVersion
	}{
		{epoch: 0, expected: spec.DataVersionPhase0},
		{epoch: 9, expected: spec.DataVersionPhase0},
		{epoch: 10, expected: spec.DataVersionAltair},
		{epoch: 15, expected: spec.DataVersionAltair},
		{epoch: 20, expected: spec.DataVersionBellatrix},
		{epoch: 25, expected: spec.DataVersionBellatrix},
		{epoch: 30, expected: spec.DataVersionCapella},
		{epoch: 35, expected: spec.DataVersionCapella},
		{epoch: 40, expected: spec.DataVersionDeneb},
		{epoch: 45, expected: spec.DataVersionDeneb},
		{epoch: 50, expected: spec.DataVersionElectra},
		{epoch: 55, expected: spec.DataVersionElectra},
	}

	for _, tc := range tests {
		got := config.DataVersion(tc.epoch)
		if got != tc.expected {
			t.Errorf("DataVersion(%d): expected %v, got %v", tc.epoch, tc.expected, got)
		}
	}
}
