package goclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/beacon/goclient/tests"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

func TestHealthy(t *testing.T) {
	t.Run("sync distance larger than allowed", func(t *testing.T) {
		syncData := &v1.SyncState{
			SyncDistance: phase0.Slot(3),
			IsSyncing:    true,
		}
		err := runHealthyTest(t, syncData, 2)
		require.ErrorIs(t, err, errSyncing)
	})

	t.Run("sync distance within allowed limits", func(t *testing.T) {
		syncData := &v1.SyncState{
			SyncDistance: phase0.Slot(3),
			IsSyncing:    true,
		}

		err := runHealthyTest(t, syncData, 3)
		require.NoError(t, err)
	})
}

func runHealthyTest(t *testing.T, syncData *v1.SyncState, syncDistanceTolerance phase0.Slot) error {
	const (
		commonTimeout = 100 * time.Millisecond
		longTimeout   = 500 * time.Millisecond
	)

	replaceSyncing := atomic.Bool{}

	mockResponses := tests.MockResponses()
	mockServer := tests.MockServer(func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
		if r.URL.Path == syncingPath && replaceSyncing.Load() {
			output := struct {
				Data *v1.SyncState `json:"data"`
			}{
				Data: syncData,
			}
			rawData, err := json.Marshal(output)
			if err != nil {
				return nil, err
			}

			return rawData, nil
		}

		return mockResponses[r.URL.Path], nil
	})
	c, err := mockClient(t.Context(), mockServer.URL, commonTimeout, longTimeout)
	require.NoError(t, err)

	client := c.(*GoClient)
	err = client.Healthy(t.Context())
	require.NoError(t, err)

	client.syncDistanceTolerance = syncDistanceTolerance

	replaceSyncing.Store(true)

	return client.Healthy(t.Context())
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
		undialableServer := tests.MockServer(func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
			time.Sleep(commonTimeout * 2)
			return resp, nil
		})
		_, err := mockClient(t.Context(), undialableServer.URL, commonTimeout, longTimeout)
		require.ErrorContains(t, err, "client is not active")
	}

	// Too slow to respond to the Validators request.
	{
		unresponsiveServer := tests.MockServer(func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
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
		unresponsiveServer := tests.MockServer(func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
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
		fastServer := tests.MockServer(func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
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
