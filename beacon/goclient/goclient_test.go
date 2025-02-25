package goclient

import (
	"context"
	"encoding/json"
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
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/slotticker"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

func TestHealthy(t *testing.T) {
	const (
		commonTimeout = 100 * time.Millisecond
		longTimeout   = 500 * time.Millisecond
	)

	ctx := context.Background()
	undialableServer := mockServer(t, nil)
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
		undialableServer := mockServer(t, func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
			time.Sleep(commonTimeout * 2)
			return resp, nil
		})
		_, err := mockClient(ctx, undialableServer.URL, commonTimeout, longTimeout)
		require.ErrorContains(t, err, "client is not active")
	}

	// Too slow to respond to the Validators request.
	{
		unresponsiveServer := mockServer(t, func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
			switch r.URL.Path {
			case "/eth/v2/debug/beacon/states/head":
				time.Sleep(longTimeout / 2)
			case "/eth/v1/beacon/states/head/validators":
				time.Sleep(longTimeout * 2)
			}
			return resp, nil
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
		unresponsiveServer := mockServer(t, func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
			switch r.URL.Path {
			case "/eth/v1/validator/duties/proposer/" + fmt.Sprint(mockServerEpoch):
				time.Sleep(longTimeout * 2)
			}
			return resp, nil
		})
		client, err := mockClient(ctx, unresponsiveServer.URL, commonTimeout, longTimeout)
		require.NoError(t, err)

		_, err = client.ProposerDuties(ctx, mockServerEpoch, nil)
		require.ErrorContains(t, err, "context deadline exceeded")
	}

	// Fast enough.
	{
		fastServer := mockServer(t, func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
			time.Sleep(commonTimeout / 2)
			switch r.URL.Path {
			case "/eth/v2/debug/beacon/states/head":
				time.Sleep(longTimeout / 2)
			}
			return resp, nil
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

func TestAssertSameGenesisVersionWhenSame(t *testing.T) {
	networks := []types.BeaconNetwork{types.MainNetwork, types.HoleskyNetwork, types.PraterNetwork, types.BeaconTestNetwork}

	for _, network := range networks {
		forkVersion := phase0.Version(beacon.NewNetwork(network).ForkVersion())
		genesis := &v1.Genesis{
			GenesisForkVersion: forkVersion,
		}

		ctx := context.Background()
		callback := func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
			if r.URL.Path == "/eth/v1/beacon/genesis" {
				resp2 := json.RawMessage(fmt.Sprintf(`{"data": {
				"genesis_time": "1606824023",
				"genesis_validators_root": "0x4b363db94e286120d76eb905340fdd4e54bfe9f06bf33ff6cf5ad27f511bfe95",
				"genesis_fork_version": "%s"
			}}`, forkVersion))
				return resp2, nil
			}
			return resp, nil
		}

		server := mockServer(t, callback)
		defer server.Close()
		t.Run(fmt.Sprintf("When genesis versions are the same (%s)", string(network)), func(t *testing.T) {
			c, err := mockClientWithNetwork(ctx, server.URL, 100*time.Millisecond, 500*time.Millisecond, network)
			require.NoError(t, err, "failed to create client")
			client := c.(*GoClient)

			output, err := client.assertSameGenesisVersion(genesis)
			require.Equal(t, forkVersion, output)
			require.NoError(t, err, "failed to assert same genesis version: %s", err)
		})
	}
}

func TestAssertSameGenesisVersionWhenDifferent(t *testing.T) {
	network := types.MainNetwork
	networkVersion := phase0.Version(beacon.NewNetwork(network).ForkVersion())

	t.Run("When genesis versions are different", func(t *testing.T) {
		ctx := context.Background()
		server := mockServer(t, nil)
		defer server.Close()
		c, err := mockClientWithNetwork(ctx, server.URL, 100*time.Millisecond, 500*time.Millisecond, network)
		require.NoError(t, err, "failed to create client")
		client := c.(*GoClient)
		forkVersion := phase0.Version{0x01, 0x02, 0x03, 0x04}
		genesis := &v1.Genesis{
			GenesisForkVersion: forkVersion,
		}

		output, err := client.assertSameGenesisVersion(genesis)
		require.Equal(t, networkVersion, output, "expected genesis version to be %s, got %s", networkVersion, output)
		require.Error(t, err, "expected error when genesis versions are different")
	})
}

func mockClient(ctx context.Context, serverURL string, commonTimeout, longTimeout time.Duration) (beacon.BeaconNode, error) {
	return mockClientWithNetwork(ctx, serverURL, commonTimeout, longTimeout, types.MainNetwork)
}

func mockClientWithNetwork(ctx context.Context, serverURL string, commonTimeout, longTimeout time.Duration, network types.BeaconNetwork) (beacon.BeaconNode, error) {
	return New(
		zap.NewNop(),
		beacon.Options{
			Context:        ctx,
			Network:        beacon.NewNetwork(network),
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

type requestCallback = func(r *http.Request, resp json.RawMessage) (json.RawMessage, error)

func mockServer(t *testing.T, onRequestFn requestCallback) *httptest.Server {
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
		var err error
		if onRequestFn != nil {
			resp, err = onRequestFn(r, resp)
			require.NoError(t, err)
		}

		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(resp); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}))
}
