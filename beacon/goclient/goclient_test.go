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

	"github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/operator/slotticker"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

func TestTimeouts(t *testing.T) {
	ctx := context.Background()

	const (
		commonTimeout = 100 * time.Millisecond
		longTimeout   = 500 * time.Millisecond
	)

	// Too slow to dial.
	{
		undialableServer := mockServer(t, delays{
			BaseDelay: commonTimeout * 2,
		})
		_, err := mockClient(t, ctx, undialableServer.URL, commonTimeout, longTimeout)
		require.ErrorContains(t, err, "context deadline exceeded")
	}

	// Too slow to respond to the Validators request.
	{
		unresponsiveServer := mockServer(t, delays{
			ValidatorsDelay:  longTimeout * 2,
			BeaconStateDelay: longTimeout / 2,
		})
		client, err := mockClient(t, ctx, unresponsiveServer.URL, commonTimeout, longTimeout)
		require.NoError(t, err)

		require.NoError(t, err)
		_, err = client.(*goClient).GetValidatorData(nil) // Shouldn't call BeaconState internally.
		require.ErrorContains(t, err, "context deadline exceeded")

		duties, err := client.(*goClient).ProposerDuties(ctx, mockServerEpoch, nil)
		require.NoError(t, err)
		require.NotEmpty(t, duties)
	}

	// Too slow to respond to proposer duties request.
	{
		unresponsiveServer := mockServer(t, delays{
			ProposerDutiesDelay: longTimeout * 2,
		})
		client, err := mockClient(t, ctx, unresponsiveServer.URL, commonTimeout, longTimeout)
		require.NoError(t, err)

		_, err = client.(*goClient).ProposerDuties(ctx, mockServerEpoch, nil)
		require.ErrorContains(t, err, "context deadline exceeded")
	}

	// Fast enough.
	{
		fastServer := mockServer(t, delays{
			BaseDelay:        commonTimeout / 2,
			BeaconStateDelay: longTimeout / 2,
		})
		client, err := mockClient(t, ctx, fastServer.URL, commonTimeout, longTimeout)
		require.NoError(t, err)

		validators, err := client.(*goClient).GetValidatorData(nil)
		require.NoError(t, err)
		require.NotEmpty(t, validators)

		duties, err := client.(*goClient).ProposerDuties(ctx, mockServerEpoch, nil)
		require.NoError(t, err)
		require.NotEmpty(t, duties)
	}
}

func mockClient(t *testing.T, ctx context.Context, serverURL string, commonTimeout, longTimeout time.Duration) (beacon.BeaconNode, error) {
	return New(
		zap.NewNop(),
		beacon.Options{
			Context:        ctx,
			Network:        beacon.NewNetwork(types.MainNetwork),
			BeaconNodeAddr: serverURL,
			CommonTimeout:  commonTimeout,
			LongTimeout:    longTimeout,
		},
		nil,
		func() slotticker.SlotTicker {
			return slotticker.New(zap.NewNop(), slotticker.Config{
				SlotDuration: 12 * time.Second,
				GenesisTime:  time.Now(),
			})
		},
	)
}

type delays struct {
	BaseDelay           time.Duration
	ProposerDutiesDelay time.Duration
	BeaconStateDelay    time.Duration
	ValidatorsDelay     time.Duration
}

// epoch to use in requests to the mock server.
const mockServerEpoch = 132502

func mockServer(t *testing.T, delays delays) *httptest.Server {
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

		time.Sleep(delays.BaseDelay)
		switch r.URL.Path {
		case "/eth/v1/validator/duties/proposer/" + fmt.Sprint(mockServerEpoch):
			time.Sleep(delays.ProposerDutiesDelay)
		case "/eth/v2/debug/beacon/states/head":
			time.Sleep(delays.BeaconStateDelay)
		case "/eth/v1/beacon/states/head/validators":
			time.Sleep(delays.ValidatorsDelay)
		}

		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(resp)); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}))
}
