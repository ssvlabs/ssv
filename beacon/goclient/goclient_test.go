package goclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/beacon/goclient/mocks"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

func TestHealthy(t *testing.T) {
	const (
		commonTimeout = 100 * time.Millisecond
		longTimeout   = 500 * time.Millisecond
	)

	undialableServer := mocks.MockServer(nil)
	c, err := mockClient(t.Context(), undialableServer.URL, commonTimeout, longTimeout)
	require.NoError(t, err)

	client := c.(*GoClient)
	err = client.Healthy(t.Context())
	require.NoError(t, err)

	t.Run("sync distance larger than allowed", func(t *testing.T) {
		client.nodeSyncingFn = func(ctx context.Context, opts *api.NodeSyncingOpts) (*api.Response[*v1.SyncState], error) {
			r := new(api.Response[*v1.SyncState])
			r.Data = &v1.SyncState{
				SyncDistance: phase0.Slot(3),
				IsSyncing:    true,
			}
			return r, nil
		}

		client.syncDistanceTolerance = 2

		err = client.Healthy(t.Context())
		require.ErrorIs(t, err, errSyncing)
	})

	t.Run("sync distance within allowed limits", func(t *testing.T) {
		client.nodeSyncingFn = func(ctx context.Context, opts *api.NodeSyncingOpts) (*api.Response[*v1.SyncState], error) {
			r := new(api.Response[*v1.SyncState])
			r.Data = &v1.SyncState{
				SyncDistance: phase0.Slot(3),
				IsSyncing:    true,
			}
			return r, nil
		}

		client.syncDistanceTolerance = 3

		err = client.Healthy(t.Context())
		require.NoError(t, err)
	})
}

func TestTimeouts(t *testing.T) {
	const (
		commonTimeout = 100 * time.Millisecond
		longTimeout   = 500 * time.Millisecond
		// mockServerEpoch is the epoch to use in requests to the mock server.
		mockServerEpoch = 132502
	)

	// Too slow to dial.
	{
		undialableServer := mocks.MockServer(func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
			time.Sleep(commonTimeout * 2)
			return resp, nil
		})
		_, err := mockClient(t.Context(), undialableServer.URL, commonTimeout, longTimeout)
		require.ErrorContains(t, err, "client is not active")
	}

	// Too slow to respond to the Validators request.
	{
		unresponsiveServer := mocks.MockServer(func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
			switch r.URL.Path {
			case "/eth/v2/debug/beacon/states/head":
				time.Sleep(longTimeout / 2)
			case "/eth/v1/beacon/states/head/validators":
				time.Sleep(longTimeout * 2)
			}
			return resp, nil
		})
		client, err := mockClient(t.Context(), unresponsiveServer.URL, commonTimeout, longTimeout)
		require.NoError(t, err)

		validators, err := client.GetValidatorData(t.Context(), nil) // Should call BeaconState internally.
		require.NoError(t, err)

		var validatorKeys []phase0.BLSPubKey
		for _, v := range validators {
			validatorKeys = append(validatorKeys, v.Validator.PublicKey)
		}

		_, err = client.GetValidatorData(t.Context(), validatorKeys) // Shouldn't call BeaconState internally.
		require.ErrorContains(t, err, "context deadline exceeded")

		duties, err := client.ProposerDuties(t.Context(), mockServerEpoch, nil)
		require.NoError(t, err)
		require.NotEmpty(t, duties)
	}

	// Too slow to respond to proposer duties request.
	{
		unresponsiveServer := mocks.MockServer(func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
			switch r.URL.Path {
			case "/eth/v1/validator/duties/proposer/" + fmt.Sprint(mockServerEpoch):
				time.Sleep(longTimeout * 2)
			}
			return resp, nil
		})
		client, err := mockClient(t.Context(), unresponsiveServer.URL, commonTimeout, longTimeout)
		require.NoError(t, err)

		_, err = client.ProposerDuties(t.Context(), mockServerEpoch, nil)
		require.ErrorContains(t, err, "context deadline exceeded")
	}

	// Fast enough.
	{
		fastServer := mocks.MockServer(func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
			time.Sleep(commonTimeout / 2)
			switch r.URL.Path {
			case "/eth/v1/config/spec":
			case "/eth/v1/beacon/genesis":
			case "/eth/v1/node/syncing":
			case "/eth/v1/node/version":
			case "/eth/v2/debug/beacon/states/head":
				time.Sleep(longTimeout / 2)
			}
			return resp, nil
		})
		client, err := mockClient(t.Context(), fastServer.URL, commonTimeout, longTimeout)
		require.NoError(t, err)

		validators, err := client.GetValidatorData(t.Context(), nil)
		require.NoError(t, err)
		require.NotEmpty(t, validators)

		duties, err := client.ProposerDuties(t.Context(), mockServerEpoch, nil)
		require.NoError(t, err)
		require.NotEmpty(t, duties)
	}
}

func mockClient(ctx context.Context, serverURL string, commonTimeout, longTimeout time.Duration) (beacon.BeaconNode, error) {
	return New(
		ctx,
		zap.NewNop(),
		Options{
			BeaconNodeAddr: serverURL,
			CommonTimeout:  commonTimeout,
			LongTimeout:    longTimeout,
		},
	)
}
