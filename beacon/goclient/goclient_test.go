package goclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv-spec/types"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/slotticker"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHealthy(t *testing.T) {
	const (
		commonTimeout = 100 * time.Millisecond
		longTimeout   = 500 * time.Millisecond
	)

	ctx := context.Background()
	undialableServer := mockServer(t, func(r *http.Request) error { return nil })
	c, err := mockClient(ctx, undialableServer.URL, commonTimeout, longTimeout)
	require.NoError(t, err)

	client := c.(*GoClient)
	err = client.Healthy(ctx)
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

		err = client.Healthy(ctx)
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

		err = client.Healthy(ctx)
		require.NoError(t, err)
	})

	t.Run("within distance/time allowance", func(t *testing.T) {
		lh := time.Now().Add(-59 * time.Second)
		client.lastHealthy = lh
		client.nodeSyncingFn = func(ctx context.Context, opts *api.NodeSyncingOpts) (*api.Response[*v1.SyncState], error) {
			r := new(api.Response[*v1.SyncState])
			r.Data = &v1.SyncState{
				IsSyncing: true,
			}
			return r, nil
		}

		err = client.Healthy(ctx)
		require.NoError(t, err)
		assert.True(t, client.lastHealthy.After(lh))
	})

	t.Run("outside time allowance", func(t *testing.T) {
		lh := time.Now().Add(-time.Minute)
		client.lastHealthy = lh
		client.nodeSyncingFn = func(ctx context.Context, opts *api.NodeSyncingOpts) (*api.Response[*v1.SyncState], error) {
			r := new(api.Response[*v1.SyncState])
			r.Data = &v1.SyncState{
				IsSyncing: true,
			}
			return r, nil
		}

		err = client.Healthy(ctx)
		require.ErrorIs(t, err, errSyncing)
		assert.True(t, client.lastHealthy == lh)
	})

	t.Run("sync error overriden if within time limits", func(t *testing.T) {
		client.lastHealthy = time.Now().Add(-59 * time.Second)
		client.nodeSyncingFn = func(ctx context.Context, opts *api.NodeSyncingOpts) (*api.Response[*v1.SyncState], error) {
			return nil, errors.New("some err")
		}

		err = client.Healthy(ctx)
		require.NoError(t, err)
	})
	t.Run("sync nil response err overriden if within time limits", func(t *testing.T) {
		client.lastHealthy = time.Now().Add(-59 * time.Second)
		client.nodeSyncingFn = func(ctx context.Context, opts *api.NodeSyncingOpts) (*api.Response[*v1.SyncState], error) {
			return nil, nil
		}

		err = client.Healthy(ctx)
		require.NoError(t, err)
	})
	t.Run("sync nil response data err overriden if within time ", func(t *testing.T) {
		client.lastHealthy = time.Now().Add(-59 * time.Second)
		client.nodeSyncingFn = func(ctx context.Context, opts *api.NodeSyncingOpts) (*api.Response[*v1.SyncState], error) {
			return new(api.Response[*v1.SyncState]), nil
		}

		err = client.Healthy(ctx)
		require.NoError(t, err)
	})
}

func TestTimeouts(t *testing.T) {
	ctx := context.Background()

	const (
		commonTimeout = 100 * time.Millisecond
		longTimeout   = 500 * time.Millisecond
		// mockServerEpoch is the epoch to use in requests to the mock server.
		mockServerEpoch = 132502
	)

	// Too slow to dial.
	{
		undialableServer := mockServer(t, func(r *http.Request) error {
			time.Sleep(commonTimeout * 2)
			return nil
		})
		_, err := mockClient(ctx, undialableServer.URL, commonTimeout, longTimeout)
		require.ErrorContains(t, err, "client is not active")
	}

	// Too slow to respond to the Validators request.
	{
		unresponsiveServer := mockServer(t, func(r *http.Request) error {
			switch r.URL.Path {
			case "/eth/v2/debug/beacon/states/head":
				time.Sleep(longTimeout / 2)
			case "/eth/v1/beacon/states/head/validators":
				time.Sleep(longTimeout * 2)
			}
			return nil
		})
		client, err := mockClient(ctx, unresponsiveServer.URL, commonTimeout, longTimeout)
		require.NoError(t, err)

		validators, err := client.GetValidatorData(nil) // Should call BeaconState internally.
		require.NoError(t, err)

		var validatorKeys []phase0.BLSPubKey
		for _, v := range validators {
			validatorKeys = append(validatorKeys, v.Validator.PublicKey)
		}

		_, err = client.GetValidatorData(validatorKeys) // Shouldn't call BeaconState internally.
		require.ErrorContains(t, err, "context deadline exceeded")

		duties, err := client.ProposerDuties(ctx, mockServerEpoch, nil)
		require.NoError(t, err)
		require.NotEmpty(t, duties)
	}

	// Too slow to respond to proposer duties request.
	{
		unresponsiveServer := mockServer(t, func(r *http.Request) error {
			switch r.URL.Path {
			case "/eth/v1/validator/duties/proposer/" + fmt.Sprint(mockServerEpoch):
				time.Sleep(longTimeout * 2)
			}
			return nil
		})
		client, err := mockClient(ctx, unresponsiveServer.URL, commonTimeout, longTimeout)
		require.NoError(t, err)

		_, err = client.ProposerDuties(ctx, mockServerEpoch, nil)
		require.ErrorContains(t, err, "context deadline exceeded")
	}

	// Fast enough.
	{
		fastServer := mockServer(t, func(r *http.Request) error {
			time.Sleep(commonTimeout / 2)
			switch r.URL.Path {
			case "/eth/v2/debug/beacon/states/head":
				time.Sleep(longTimeout / 2)
			}
			return nil
		})
		client, err := mockClient(ctx, fastServer.URL, commonTimeout, longTimeout)
		require.NoError(t, err)

		validators, err := client.GetValidatorData(nil)
		require.NoError(t, err)
		require.NotEmpty(t, validators)

		duties, err := client.ProposerDuties(ctx, mockServerEpoch, nil)
		require.NoError(t, err)
		require.NotEmpty(t, duties)
	}
}

func mockClient(ctx context.Context, serverURL string, commonTimeout, longTimeout time.Duration) (beacon.BeaconNode, error) {
	return New(
		zap.NewNop(),
		beacon.Options{
			Context:        ctx,
			Network:        beacon.NewNetwork(types.MainNetwork),
			BeaconNodeAddr: serverURL,
			CommonTimeout:  commonTimeout,
			LongTimeout:    longTimeout,
		},
		operatordatastore.New(&registrystorage.OperatorData{ID: 1}),
		func() slotticker.SlotTicker {
			return slotticker.New(zap.NewNop(), slotticker.Config{
				SlotDuration: 12 * time.Second,
				GenesisTime:  time.Now(),
			})
		},
	)
}

func mockServer(t *testing.T, onRequestFn func(r *http.Request) error) *httptest.Server {
	var mockResponses map[string]json.RawMessage
	f, err := os.Open("testdata/mock-beacon-responses.json")
	require.NoError(t, err)
	require.NoError(t, json.NewDecoder(f).Decode(&mockResponses))

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("mock server handling request: %s", r.URL.Path)

		resp, ok := mockResponses[r.URL.Path]
		if !ok {
			require.FailNowf(t, "unexpected request", "unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		err := onRequestFn(r)
		require.NoError(t, err)

		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(resp); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}))
}
