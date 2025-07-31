package goclient

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/beacon/goclient/mocks"
	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/networkconfig"
)

const (
	genesisPath = "/eth/v1/beacon/genesis"
	specPath    = "/eth/v1/config/spec"
	syncingPath = "/eth/v1/node/syncing"
)

func Test_genesisForClient(t *testing.T) {
	ctx := t.Context()

	logger := logging.TestLogger(t)

	t.Run("success", func(t *testing.T) {
		mockServer := mocks.NewServer(func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
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
						"CONFIG_NAME": "mainnet",
						"GENESIS_FORK_VERSION": "0x00000000",
						"ALTAIR_FORK_VERSION": "0x01000000",
						"ALTAIR_FORK_EPOCH": "74240",
						"BELLATRIX_FORK_VERSION": "0x02000000",
						"BELLATRIX_FORK_EPOCH": "144896",
						"CAPELLA_FORK_VERSION": "0x03000000",
						"CAPELLA_FORK_EPOCH": "194048",
						"DENEB_FORK_VERSION": "0x04000000",
						"DENEB_FORK_EPOCH": "269568",
						"ELECTRA_FORK_VERSION": "0x05000000",
						"ELECTRA_FORK_EPOCH": "364032",
						"FULU_FORK_VERSION": "0x06000000",
						"FULU_FORK_EPOCH": "18446744073709551615",
						"MIN_GENESIS_TIME": "1606824000",
						"SECONDS_PER_SLOT": "12",
						"SLOTS_PER_EPOCH": "32",
						"EPOCHS_PER_SYNC_COMMITTEE_PERIOD": "256",
						"SYNC_COMMITTEE_SIZE": "512",
						"SYNC_COMMITTEE_SUBNET_COUNT": "4",
						"TARGET_AGGREGATORS_PER_COMMITTEE": "16",
						"TARGET_AGGREGATORS_PER_SYNC_SUBCOMMITTEE": "16",
						"INTERVALS_PER_SLOT": "3"
					}
				}`), nil
			}
			return resp, nil
		})
		defer mockServer.Close()

		client, err := New(
			ctx,
			logger,
			Options{
				BeaconConfig:   networkconfig.TestNetwork.BeaconConfig,
				BeaconNodeAddr: mockServer.URL,
				CommonTimeout:  100 * time.Millisecond,
				LongTimeout:    500 * time.Millisecond,
			},
		)
		require.NoError(t, err)

		genesis, err := genesisForClient(ctx, logger, client.multiClient)
		require.NoError(t, err)
		require.NotNil(t, genesis)

		expectedTime := time.Unix(1606824023, 0)
		require.Equal(t, expectedTime, genesis.GenesisTime)
	})

	t.Run("nil_data", func(t *testing.T) {
		mockServer := mocks.NewServer(func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
			if r.URL.Path == genesisPath {
				return json.RawMessage(`{"data": null}`), nil
			}
			return resp, nil
		})
		defer mockServer.Close()

		client, err := New(
			ctx,
			logger,
			Options{
				BeaconConfig:   networkconfig.TestNetwork.BeaconConfig,
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
		mockServer := mocks.NewServer(func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
			if r.URL.Path == genesisPath {
				return json.RawMessage(`malformed`), nil
			}
			return resp, nil
		})
		defer mockServer.Close()

		client, err := New(
			ctx,
			logger,
			Options{
				BeaconConfig:   networkconfig.TestNetwork.BeaconConfig,
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
