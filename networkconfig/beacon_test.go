package networkconfig

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// TestDataVersion verifies that DataVersion returns the correct version based on fork epochs.
func TestDataVersion(t *testing.T) {
	config := &BeaconConfig{
		Forks: map[spec.DataVersion]phase0.Fork{
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
		got, _ := config.ForkAtEpoch(tc.epoch)
		if got != tc.expected {
			t.Errorf("DataVersion(%d): expected %v, got %v", tc.epoch, tc.expected, got)
		}
	}
}

// TODO: fix the test below
//
//func TestCurrentFork(t *testing.T) {
//	ctx := context.Background()
//
//	beaconConfig := networkconfig.Mainnet.BeaconConfig
//
//	t.Run("success", func(t *testing.T) {
//		mockServer := tests.MockServer(func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
//			if r.URL.Path == forkSchedulePath {
//				return json.RawMessage(`{
//					"data": [
//						{
//							"previous_version": "0x00010203",
//							"current_version": "0x04050607",
//							"epoch": "100"
//						},
//						{
//							"previous_version": "0x04050607",
//							"current_version": "0x08090a0b",
//							"epoch": "150"
//						},
//						{
//							"previous_version": "0x08090a0b",
//							"current_version": "0x0c0d0e0f",
//							"epoch": "300"
//						}
//					]
//				}`), nil
//			}
//			return resp, nil
//		})
//		defer mockServer.Close()
//
//		client, err := New(
//			zap.NewNop(),
//			Options{
//				Context:        ctx,
//				BeaconConfig:   beaconConfig,
//				BeaconNodeAddr: mockServer.URL,
//				CommonTimeout:  100 * time.Millisecond,
//				LongTimeout:    500 * time.Millisecond,
//			},
//		)
//		require.NoError(t, err)
//
//		currentFork, err := client.ForkAtEpoch(ctx, 200)
//		require.NoError(t, err)
//		require.NotNil(t, currentFork)
//
//		require.EqualValues(t, 150, currentFork.Epoch)
//		require.Equal(t, phase0.Version{0x04, 0x05, 0x06, 0x07}, currentFork.PreviousVersion)
//		require.Equal(t, phase0.Version{0x08, 0x09, 0x0a, 0x0b}, currentFork.CurrentVersion)
//	})
//
//	t.Run("nil_data", func(t *testing.T) {
//		mockServer := tests.MockServer(func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
//			if r.URL.Path == forkSchedulePath {
//				return json.RawMessage(`{"data": null}`), nil
//			}
//			return resp, nil
//		})
//		defer mockServer.Close()
//
//		client, err := New(
//			zap.NewNop(),
//			Options{
//				Context:        ctx,
//				BeaconConfig:   beaconConfig,
//				BeaconNodeAddr: mockServer.URL,
//				CommonTimeout:  100 * time.Millisecond,
//				LongTimeout:    500 * time.Millisecond,
//			},
//		)
//		require.NoError(t, err)
//
//		_, err = client.ForkAtEpoch(ctx, 1)
//		require.Error(t, err)
//		require.Contains(t, err.Error(), "fork schedule response data is nil")
//	})
//
//	t.Run("no_current_fork", func(t *testing.T) {
//		mockServer := tests.MockServer(func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
//			if r.URL.Path == forkSchedulePath {
//				return json.RawMessage(`{
//					"data": [
//						{
//							"previous_version": "0x00010203",
//							"current_version": "0x04050607",
//							"epoch": "200"
//						},
//						{
//							"previous_version": "0x04050607",
//							"current_version": "0x08090a0b",
//							"epoch": "300"
//						}
//					]
//				}`), nil
//			}
//			return resp, nil
//		})
//		defer mockServer.Close()
//
//		client, err := New(
//			zap.NewNop(),
//			Options{
//				Context:        ctx,
//				BeaconConfig:   beaconConfig,
//				BeaconNodeAddr: mockServer.URL,
//				CommonTimeout:  100 * time.Millisecond,
//				LongTimeout:    500 * time.Millisecond,
//			},
//		)
//		require.NoError(t, err)
//
//		_, err = client.ForkAtEpoch(ctx, 100)
//		require.Error(t, err)
//		require.Contains(t, err.Error(), "could not find fork at epoch 100")
//	})
//}
//
