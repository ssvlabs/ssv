package goclient

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/beacon/goclient/tests"
	"github.com/ssvlabs/ssv/networkconfig"
)

const (
	genesisPath = "/eth/v1/beacon/genesis"
	specPath    = "/eth/v1/config/spec"
)

func TestGenesis(t *testing.T) {
	ctx := t.Context()

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
			if r.URL.Path == specPath {
				return json.RawMessage(`{
					"data": {
						"CONFIG_NAME": "holesky",
						"GENESIS_FORK_VERSION": "0x00000000",
						"CAPELLA_FORK_VERSION": "0x04017000",
						"MIN_GENESIS_TIME": "1695902100",
						"SECONDS_PER_SLOT": "12",
						"SLOTS_PER_EPOCH": "32",
						"EPOCHS_PER_SYNC_COMMITTEE_PERIOD": "256",
						"SYNC_COMMITTEE_SIZE": "512",
						"SYNC_COMMITTEE_SUBNET_COUNT": "4",
						"TARGET_AGGREGATORS_PER_COMMITTEE": "16",
						"TARGET_AGGREGATORS_PER_SYNC_SUBCOMMITTEE": "16",
						"INTERVALS_PER_SLOT": "3",
						"ALTAIR_FORK_EPOCH": "74240",
						"BELLATRIX_FORK_EPOCH": "144896",
						"CAPELLA_FORK_EPOCH": "194048",
						"DENEB_FORK_EPOCH": "269568",
						"ELECTRA_FORK_EPOCH": "18446744073709551615"
					}
				}`), nil
			}
			return resp, nil
		})
		defer mockServer.Close()

		client, err := New(
			ctx,
			zap.NewNop(),
			Options{
				BeaconConfig:   networkconfig.Mainnet.BeaconConfig,
				BeaconNodeAddr: mockServer.URL,
				CommonTimeout:  100 * time.Millisecond,
				LongTimeout:    500 * time.Millisecond,
			},
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
			ctx,
			zap.NewNop(),
			Options{
				BeaconConfig:   networkconfig.Mainnet.BeaconConfig,
				BeaconNodeAddr: mockServer.URL,
				CommonTimeout:  100 * time.Millisecond,
				LongTimeout:    500 * time.Millisecond,
			},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "timed out awaiting config initialization") // node cannot initialize if it cannot get genesis
		require.Nil(t, client)
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
			ctx,
			zap.NewNop(),
			Options{
				BeaconConfig:   networkconfig.Mainnet.BeaconConfig,
				BeaconNodeAddr: mockServer.URL,
				CommonTimeout:  100 * time.Millisecond,
				LongTimeout:    500 * time.Millisecond,
			},
		)
		require.Error(t, err)
		require.Contains(t, err.Error(), "timed out awaiting config initialization") // node cannot initialize if it cannot get genesis
		require.Nil(t, client)
	})
}
