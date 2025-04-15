package goclient

import (
	"strings"
	"testing"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/networkconfig"
)

// TestDataVersion verifies that DataVersion returns the correct version based on fork epochs.
func TestDataVersion(t *testing.T) {
	// Create a client with preset fork epochs.
	client := &GoClient{
		beaconConfig: &networkconfig.Beacon{
			ForkEpochs: &networkconfig.ForkEpochs{
				Altair:    phase0.Epoch(10),
				Bellatrix: phase0.Epoch(20),
				Capella:   phase0.Epoch(30),
				Deneb:     phase0.Epoch(40),
				Electra:   phase0.Epoch(50),
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
		got := client.beaconConfig.DataVersion(tc.epoch)
		if got != tc.expected {
			t.Errorf("DataVersion(%d): expected %v, got %v", tc.epoch, tc.expected, got)
		}
	}
}

// TestCheckForkValues verifies the checkForkValues function across various scenarios.
func TestCheckForkValues(t *testing.T) {
	tests := []struct {
		name string
		// input response and expected outcomes
		response    *api.Response[map[string]any]
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
			name: "nil data",
			response: &api.Response[map[string]any]{
				Data: nil,
			},
			expectedErr: "spec response data is nil",
		},
		{
			name: "missing ALTAIR",
			response: &api.Response[map[string]any]{
				Data: map[string]any{
					"BELLATRIX_FORK_EPOCH": uint64(20),
					"CAPELLA_FORK_EPOCH":   uint64(30),
					"DENEB_FORK_EPOCH":     uint64(40),
				},
			},
			expectedErr: "ALTAIR_FORK_EPOCH is not known by chain",
		},
		{
			name: "invalid type for ALTAIR",
			response: &api.Response[map[string]any]{
				Data: map[string]any{
					"ALTAIR_FORK_EPOCH":    "not a uint",
					"BELLATRIX_FORK_EPOCH": uint64(20),
					"CAPELLA_FORK_EPOCH":   uint64(30),
					"DENEB_FORK_EPOCH":     uint64(40),
				},
			},
			expectedErr: "failed to decode ALTAIR_FORK_EPOCH",
		},
		{
			name: "valid update with initial zeros and electra provided",
			response: &api.Response[map[string]any]{
				Data: map[string]any{
					"ALTAIR_FORK_EPOCH":    uint64(10),
					"BELLATRIX_FORK_EPOCH": uint64(20),
					"CAPELLA_FORK_EPOCH":   uint64(30),
					"DENEB_FORK_EPOCH":     uint64(40),
					"ELECTRA_FORK_EPOCH":   uint64(50),
				},
			},
			expectedAltair:    phase0.Epoch(10),
			expectedBellatrix: phase0.Epoch(20),
			expectedCapella:   phase0.Epoch(30),
			expectedDeneb:     phase0.Epoch(40),
			expectedElectra:   phase0.Epoch(50),
		},
		{
			name: "optional ELECTRA not provided, set to FarFutureEpoch",
			response: &api.Response[map[string]any]{
				Data: map[string]any{
					"ALTAIR_FORK_EPOCH":    uint64(10),
					"BELLATRIX_FORK_EPOCH": uint64(20),
					"CAPELLA_FORK_EPOCH":   uint64(30),
					"DENEB_FORK_EPOCH":     uint64(40),
				},
			},
			expectedAltair:    phase0.Epoch(10),
			expectedBellatrix: phase0.Epoch(20),
			expectedCapella:   phase0.Epoch(30),
			expectedDeneb:     phase0.Epoch(40),
			expectedElectra:   networkconfig.FarFutureEpoch,
		},
	}

	for _, tc := range tests {
		tc := tc // capture range variable
		t.Run(tc.name, func(t *testing.T) {
			// Create a client with initial fork values.
			client := &GoClient{
				beaconConfig: &networkconfig.Beacon{
					ForkEpochs: &networkconfig.ForkEpochs{
						Altair:    phase0.Epoch(10),
						Bellatrix: phase0.Epoch(20),
						Capella:   phase0.Epoch(30),
						Deneb:     phase0.Epoch(40),
						Electra:   phase0.Epoch(50),
					},
				},
			}

			forkEpochs, err := client.getForkEpochs(tc.response)
			if tc.expectedErr != "" {
				require.EqualError(t, err, tc.expectedErr)
				if err == nil {
					t.Fatalf("expected error containing %q but got nil", tc.expectedErr)
				}
				if !strings.Contains(err.Error(), tc.expectedErr) {
					t.Fatalf("expected error containing %q, got %q", tc.expectedErr, err.Error())
				}
				return
			}
			require.NoError(t, err)

			require.EqualValues(t, forkEpochs.Altair, tc.expectedAltair)
			require.EqualValues(t, forkEpochs.Bellatrix, tc.expectedBellatrix)
			require.EqualValues(t, forkEpochs.Capella, tc.expectedCapella)
			require.EqualValues(t, forkEpochs.Deneb, tc.expectedDeneb)
			require.EqualValues(t, forkEpochs.Electra, tc.expectedElectra)
		})
	}
}
