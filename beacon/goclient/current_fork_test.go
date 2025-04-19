package goclient

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/beacon/goclient/tests"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

const forkSchedulePath = "/eth/v1/config/fork_schedule"

func TestCurrentFork(t *testing.T) {
	ctx := context.Background()

	network := beacon.NewNetwork(types.MainNetwork)

	t.Run("success", func(t *testing.T) {
		mockServer := tests.MockServer(func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
			if r.URL.Path == forkSchedulePath {
				return json.RawMessage(`{
					"data": [
						{
							"previous_version": "0x00010203",
							"current_version": "0x04050607",
							"epoch": "100"
						},
						{
							"previous_version": "0x04050607",
							"current_version": "0x08090a0b",
							"epoch": "150"
						},
						{
							"previous_version": "0x08090a0b",
							"current_version": "0x0c0d0e0f",
							"epoch": "300"
						}
					]
				}`), nil
			}
			return resp, nil
		})
		defer mockServer.Close()

		client, err := New(
			zap.NewNop(),
			Options{
				Context:        ctx,
				Network:        network,
				BeaconNodeAddr: mockServer.URL,
				CommonTimeout:  100 * time.Millisecond,
				LongTimeout:    500 * time.Millisecond,
			},
			tests.MockSlotTickerProvider,
		)
		require.NoError(t, err)

		currentFork, err := client.ForkAtEpoch(ctx, 200)
		require.NoError(t, err)
		require.NotNil(t, currentFork)

		require.EqualValues(t, 150, currentFork.Epoch)
		require.Equal(t, phase0.Version{0x04, 0x05, 0x06, 0x07}, currentFork.PreviousVersion)
		require.Equal(t, phase0.Version{0x08, 0x09, 0x0a, 0x0b}, currentFork.CurrentVersion)
	})

	t.Run("nil_data", func(t *testing.T) {
		mockServer := tests.MockServer(func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
			if r.URL.Path == forkSchedulePath {
				return json.RawMessage(`{"data": null}`), nil
			}
			return resp, nil
		})
		defer mockServer.Close()

		client, err := New(
			zap.NewNop(),
			Options{
				Context:        ctx,
				Network:        network,
				BeaconNodeAddr: mockServer.URL,
				CommonTimeout:  100 * time.Millisecond,
				LongTimeout:    500 * time.Millisecond,
			},
			tests.MockSlotTickerProvider,
		)
		require.NoError(t, err)

		_, err = client.ForkAtEpoch(ctx, 1)
		require.Error(t, err)
		require.Contains(t, err.Error(), "fork schedule response data is nil")
	})

	t.Run("no_current_fork", func(t *testing.T) {
		mockServer := tests.MockServer(func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
			if r.URL.Path == forkSchedulePath {
				return json.RawMessage(`{
					"data": [
						{
							"previous_version": "0x00010203",
							"current_version": "0x04050607",
							"epoch": "200"
						},
						{
							"previous_version": "0x04050607",
							"current_version": "0x08090a0b",
							"epoch": "300"
						}
					]
				}`), nil
			}
			return resp, nil
		})
		defer mockServer.Close()

		client, err := New(
			zap.NewNop(),
			Options{
				Context:        ctx,
				Network:        network,
				BeaconNodeAddr: mockServer.URL,
				CommonTimeout:  100 * time.Millisecond,
				LongTimeout:    500 * time.Millisecond,
			},
			tests.MockSlotTickerProvider,
		)
		require.NoError(t, err)

		_, err = client.ForkAtEpoch(ctx, 100)
		require.Error(t, err)
		require.Contains(t, err.Error(), "could not find fork at epoch 100")
	})
}
