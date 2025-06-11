package goclient

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/beacon/goclient/tests"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

const genesisPath = "/eth/v1/beacon/genesis"

func TestGenesis(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		mockServer := tests.MockServer(func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
			if r.URL.Path == genesisPath {
				return json.RawMessage(`{
					"data": {
						"genesis_time": "1606824023",
						"genesis_validators_root": "0x4b363db94e28612020049ce3795b0252c16c4241df2bc9ef221abde47527c0d0",
						"genesis_fork_version": "0x00000000"
					}
				}`), nil
			}
			return resp, nil
		})
		defer mockServer.Close()

		client, err := New(
			zap.NewNop(),
			Options{
				Context:        ctx,
				Network:        beacon.NewNetwork(types.MainNetwork),
				BeaconNodeAddr: mockServer.URL,
				CommonTimeout:  100 * time.Millisecond,
				LongTimeout:    500 * time.Millisecond,
			},
			tests.MockSlotTickerProvider,
		)
		require.NoError(t, err)

		genesis, err := client.Genesis(ctx)
		require.NoError(t, err)
		require.NotNil(t, genesis)

		expectedTime := time.Unix(1606824023, 0)
		require.Equal(t, expectedTime, genesis.GenesisTime)
	})

	t.Run("nil_data", func(t *testing.T) {
		mockServer := tests.MockServer(func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
			if r.URL.Path == genesisPath {
				return json.RawMessage(`{"data": null}`), nil
			}
			return resp, nil
		})
		defer mockServer.Close()

		client, err := New(
			zap.NewNop(),
			Options{
				Context:        ctx,
				Network:        beacon.NewNetwork(types.MainNetwork),
				BeaconNodeAddr: mockServer.URL,
				CommonTimeout:  100 * time.Millisecond,
				LongTimeout:    500 * time.Millisecond,
			},
			tests.MockSlotTickerProvider,
		)
		require.NoError(t, err)

		_, err = client.Genesis(ctx)
		require.Error(t, err)
		require.Contains(t, err.Error(), "genesis response data is nil")
	})

	t.Run("error", func(t *testing.T) {
		mockServer := tests.MockServer(func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
			if r.URL.Path == genesisPath {
				return json.RawMessage(`malformed`), nil
			}
			return resp, nil
		})
		defer mockServer.Close()

		client, err := New(
			zap.NewNop(),
			Options{
				Context:        ctx,
				Network:        beacon.NewNetwork(types.MainNetwork),
				BeaconNodeAddr: mockServer.URL,
				CommonTimeout:  100 * time.Millisecond,
				LongTimeout:    500 * time.Millisecond,
			},
			tests.MockSlotTickerProvider,
		)
		require.NoError(t, err)

		_, err = client.Genesis(ctx)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to request genesis")
	})
}
