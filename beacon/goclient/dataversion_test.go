package goclient

import (
	"strings"
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// TestDataVersion verifies that DataVersion returns the correct version based on fork epochs.
func TestDataVersion(t *testing.T) {
	// Create a client with preset fork epochs.
	client := &GoClient{
		ForkEpochAltair:    phase0.Epoch(10),
		ForkEpochBellatrix: phase0.Epoch(20),
		ForkEpochCapella:   phase0.Epoch(30),
		ForkEpochDeneb:     phase0.Epoch(40),
		ForkEpochElectra:   phase0.Epoch(50),
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
		got := client.DataVersion(tc.epoch)
		if got != tc.expected {
			t.Errorf("DataVersion(%d): expected %v, got %v", tc.epoch, tc.expected, got)
		}
	}
}

// TestCheckForkValues verifies the checkForkValues function across various scenarios.
func TestCheckForkValues(t *testing.T) {
	tests := []struct {
		name string
		// initial fork values
		initialAltair, initialBellatrix, initialCapella,
		initialDeneb, initialElectra phase0.Epoch
		// input response and expected outcomes
		response    map[string]any
		expectedErr string
		expectedAltair, expectedBellatrix, expectedCapella,
		expectedDeneb, expectedElectra phase0.Epoch
	}{
		{
			name:        "nil response",
			response:    nil,
			expectedErr: "spec response is nil",
		},
		{
			name:             "missing ALTAIR",
			initialAltair:    FarFutureEpoch,
			initialBellatrix: FarFutureEpoch,
			initialCapella:   FarFutureEpoch,
			initialDeneb:     FarFutureEpoch,
			initialElectra:   FarFutureEpoch,
			response: map[string]any{
				"BELLATRIX_FORK_EPOCH": uint64(20),
				"CAPELLA_FORK_EPOCH":   uint64(30),
				"DENEB_FORK_EPOCH":     uint64(40),
			},
			expectedErr: "ALTAIR fork epoch not known by chain",
		},
		{
			name:             "invalid type for ALTAIR",
			initialAltair:    FarFutureEpoch,
			initialBellatrix: FarFutureEpoch,
			initialCapella:   FarFutureEpoch,
			initialDeneb:     FarFutureEpoch,
			initialElectra:   FarFutureEpoch,
			response: map[string]any{
				"ALTAIR_FORK_EPOCH":    "not a uint",
				"BELLATRIX_FORK_EPOCH": uint64(20),
				"CAPELLA_FORK_EPOCH":   uint64(30),
				"DENEB_FORK_EPOCH":     uint64(40),
			},
			expectedErr: "failed to decode ALTAIR fork epoch",
		},
		{
			name:             "valid update with initial zeros and electra provided",
			initialAltair:    FarFutureEpoch,
			initialBellatrix: FarFutureEpoch,
			initialCapella:   FarFutureEpoch,
			initialDeneb:     FarFutureEpoch,
			initialElectra:   FarFutureEpoch,
			response: map[string]any{
				"ALTAIR_FORK_EPOCH":    uint64(10),
				"BELLATRIX_FORK_EPOCH": uint64(20),
				"CAPELLA_FORK_EPOCH":   uint64(30),
				"DENEB_FORK_EPOCH":     uint64(40),
				"ELECTRA_FORK_EPOCH":   uint64(50),
			},
			expectedAltair:    phase0.Epoch(10),
			expectedBellatrix: phase0.Epoch(20),
			expectedCapella:   phase0.Epoch(30),
			expectedDeneb:     phase0.Epoch(40),
			expectedElectra:   phase0.Epoch(50),
		},
		{
			name:             "optional ELECTRA not provided, remains unchanged",
			initialAltair:    FarFutureEpoch,
			initialBellatrix: FarFutureEpoch,
			initialCapella:   FarFutureEpoch,
			initialDeneb:     FarFutureEpoch,
			initialElectra:   FarFutureEpoch,
			response: map[string]any{
				"ALTAIR_FORK_EPOCH":    uint64(10),
				"BELLATRIX_FORK_EPOCH": uint64(20),
				"CAPELLA_FORK_EPOCH":   uint64(30),
				"DENEB_FORK_EPOCH":     uint64(40),
			},
			expectedAltair:    phase0.Epoch(10),
			expectedBellatrix: phase0.Epoch(20),
			expectedCapella:   phase0.Epoch(30),
			expectedDeneb:     phase0.Epoch(40),
			expectedElectra:   FarFutureEpoch,
		},
		{
			name:             "optional ELECTRA provided and can't change",
			initialAltair:    10,
			initialBellatrix: 20,
			initialCapella:   30,
			initialDeneb:     40,
			initialElectra:   99,
			response: map[string]any{
				"ALTAIR_FORK_EPOCH":    uint64(10),
				"BELLATRIX_FORK_EPOCH": uint64(20),
				"CAPELLA_FORK_EPOCH":   uint64(30),
				"DENEB_FORK_EPOCH":     uint64(40),
				"ELECTRA_FORK_EPOCH":   uint64(50),
			},
			expectedAltair:    phase0.Epoch(10),
			expectedBellatrix: phase0.Epoch(20),
			expectedCapella:   phase0.Epoch(30),
			expectedDeneb:     phase0.Epoch(40),
			expectedElectra:   phase0.Epoch(50),
			expectedErr:       "new ELECTRA fork epoch (50) doesn't match current value (99)",
		},
		{
			name:             "optional ELECTRA provided, candidate greater than current",
			initialAltair:    10,
			initialBellatrix: 20,
			initialCapella:   30,
			initialDeneb:     40,
			initialElectra:   50,
			response: map[string]any{
				"ALTAIR_FORK_EPOCH":    uint64(10),
				"BELLATRIX_FORK_EPOCH": uint64(20),
				"CAPELLA_FORK_EPOCH":   uint64(30),
				"DENEB_FORK_EPOCH":     uint64(40),
				"ELECTRA_FORK_EPOCH":   uint64(60),
			},
			expectedErr: "new ELECTRA fork epoch (60) doesn't match current value (50)",
		},
	}

	for _, tc := range tests {
		tc := tc // capture range variable
		t.Run(tc.name, func(t *testing.T) {
			// Create a client with initial fork values.
			client := &GoClient{
				ForkEpochAltair:    tc.initialAltair,
				ForkEpochBellatrix: tc.initialBellatrix,
				ForkEpochCapella:   tc.initialCapella,
				ForkEpochDeneb:     tc.initialDeneb,
				ForkEpochElectra:   tc.initialElectra,
			}

			err := client.checkForkValues(tc.response)
			if tc.expectedErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q but got nil", tc.expectedErr)
				}
				if !strings.Contains(err.Error(), tc.expectedErr) {
					t.Fatalf("expected error containing %q, got %q", tc.expectedErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify that the fork epoch fields have been updated as expected.
			if client.ForkEpochAltair != tc.expectedAltair {
				t.Errorf("ForkEpochAltair: expected %d, got %d", tc.expectedAltair, client.ForkEpochAltair)
			}
			if client.ForkEpochBellatrix != tc.expectedBellatrix {
				t.Errorf("ForkEpochBellatrix: expected %d, got %d", tc.expectedBellatrix, client.ForkEpochBellatrix)
			}
			if client.ForkEpochCapella != tc.expectedCapella {
				t.Errorf("ForkEpochCapella: expected %d, got %d", tc.expectedCapella, client.ForkEpochCapella)
			}
			if client.ForkEpochDeneb != tc.expectedDeneb {
				t.Errorf("ForkEpochDeneb: expected %d, got %d", tc.expectedDeneb, client.ForkEpochDeneb)
			}
			if client.ForkEpochElectra != tc.expectedElectra {
				t.Errorf("ForkEpochElectra: expected %d, got %d", tc.expectedElectra, client.ForkEpochElectra)
			}
		})
	}
}
