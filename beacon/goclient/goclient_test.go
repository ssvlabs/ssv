package goclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/beacon/goclient/tests"
)

func TestHealthy(t *testing.T) {
	t.Parallel()

	t.Run("single client: zero sync distance, not syncing", func(t *testing.T) {
		t.Parallel()

		syncResponseList := []syncResponse{
			{
				state: &v1.SyncState{
					SyncDistance: phase0.Slot(0),
					IsSyncing:    false,
				},
			},
		}
		err := runHealthyTest(t, syncResponseList, 2, false)
		require.NoError(t, err)
	})

	t.Run("single client: sync distance within allowed limits", func(t *testing.T) {
		t.Parallel()

		syncResponseList := []syncResponse{
			{
				state: &v1.SyncState{
					SyncDistance: phase0.Slot(1),
					IsSyncing:    true,
				},
			},
		}

		err := runHealthyTest(t, syncResponseList, 2, false)
		require.NoError(t, err)
	})

	t.Run("single client: sync distance larger than allowed", func(t *testing.T) {
		t.Parallel()

		syncResponseList := []syncResponse{
			{
				state: &v1.SyncState{
					SyncDistance: phase0.Slot(3),
					IsSyncing:    true,
				},
			},
		}
		err := runHealthyTest(t, syncResponseList, 2, false)
		require.ErrorIs(t, err, errSyncing)
	})

	t.Run("multi client: both healthy", func(t *testing.T) {
		t.Parallel()

		syncResponseList := []syncResponse{
			{
				state: &v1.SyncState{
					SyncDistance: phase0.Slot(0),
					IsSyncing:    false,
				},
			},
			{
				state: &v1.SyncState{
					SyncDistance: phase0.Slot(0),
					IsSyncing:    false,
				},
			},
		}

		err := runHealthyTest(t, syncResponseList, 2, false)
		require.NoError(t, err)
	})

	t.Run("multi client: only first healthy", func(t *testing.T) {
		t.Parallel()

		syncResponseList := []syncResponse{
			{
				state: &v1.SyncState{
					SyncDistance: phase0.Slot(0),
					IsSyncing:    false,
				},
			},
			{
				state: &v1.SyncState{
					SyncDistance: phase0.Slot(3),
					IsSyncing:    true,
				},
			},
		}

		err := runHealthyTest(t, syncResponseList, 2, false)
		require.NoError(t, err)
	})

	t.Run("multi client: only second healthy", func(t *testing.T) {
		t.Parallel()

		syncResponseList := []syncResponse{
			{
				state: &v1.SyncState{
					SyncDistance: phase0.Slot(3),
					IsSyncing:    true,
				},
			},
			{
				state: &v1.SyncState{
					SyncDistance: phase0.Slot(0),
					IsSyncing:    false,
				},
			},
		}

		err := runHealthyTest(t, syncResponseList, 2, false)
		require.NoError(t, err)
	})

	t.Run("multi client: no healthy", func(t *testing.T) {
		t.Parallel()

		syncResponseList := []syncResponse{
			{
				state: &v1.SyncState{
					SyncDistance: phase0.Slot(3),
					IsSyncing:    true,
				},
			},
			{
				state: &v1.SyncState{
					SyncDistance: phase0.Slot(4),
					IsSyncing:    true,
				},
			},
		}

		err := runHealthyTest(t, syncResponseList, 2, false)
		require.ErrorIs(t, err, errSyncing)
	})

	t.Run("multi client: both time out", func(t *testing.T) {
		t.Parallel()

		syncResponseList := []syncResponse{
			{
				state: &v1.SyncState{
					SyncDistance: phase0.Slot(0),
					IsSyncing:    false,
				},
				delay: 2 * time.Second,
			},
			{
				state: &v1.SyncState{
					SyncDistance: phase0.Slot(0),
					IsSyncing:    false,
				},
				delay: 2 * time.Second,
			},
		}

		err := runHealthyTest(t, syncResponseList, 2, false)
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("multi client: first times out", func(t *testing.T) {
		t.Parallel()

		syncResponseList := []syncResponse{
			{
				state: &v1.SyncState{
					SyncDistance: phase0.Slot(0),
					IsSyncing:    false,
				},
				delay: 2 * time.Second,
			},
			{
				state: &v1.SyncState{
					SyncDistance: phase0.Slot(0),
					IsSyncing:    false,
				},
			},
		}

		err := runHealthyTest(t, syncResponseList, 2, false)
		require.NoError(t, err)
	})

	t.Run("multi client: second times out", func(t *testing.T) {
		t.Parallel()

		syncResponseList := []syncResponse{
			{
				state: &v1.SyncState{
					SyncDistance: phase0.Slot(0),
					IsSyncing:    false,
				},
			},
			{
				state: &v1.SyncState{
					SyncDistance: phase0.Slot(0),
					IsSyncing:    false,
				},
				delay: 2 * time.Second,
			},
		}

		err := runHealthyTest(t, syncResponseList, 2, false)
		require.NoError(t, err)
	})

	t.Run("multi client: concurrent check, both synced", func(t *testing.T) {
		t.Parallel()

		syncResponseList := []syncResponse{
			{
				state: &v1.SyncState{
					SyncDistance: phase0.Slot(0),
					IsSyncing:    false,
				},
			},
			{
				state: &v1.SyncState{
					SyncDistance: phase0.Slot(0),
					IsSyncing:    false,
				},
				delay: 2 * time.Second,
			},
		}

		err := runHealthyTest(t, syncResponseList, 2, false)
		require.NoError(t, err)
	})

	t.Run("multi client: concurrent check, only first synced", func(t *testing.T) {
		t.Parallel()

		syncResponseList := []syncResponse{
			{
				state: &v1.SyncState{
					SyncDistance: phase0.Slot(0),
					IsSyncing:    false,
				},
			},
			{
				state: &v1.SyncState{
					SyncDistance: phase0.Slot(3),
					IsSyncing:    true,
				},
			},
		}

		err := runHealthyTest(t, syncResponseList, 2, false)
		require.NoError(t, err)
	})

	t.Run("multi client: concurrent check, only second synced", func(t *testing.T) {
		t.Parallel()

		syncResponseList := []syncResponse{
			{
				state: &v1.SyncState{
					SyncDistance: phase0.Slot(3),
					IsSyncing:    true,
				},
			},
			{
				state: &v1.SyncState{
					SyncDistance: phase0.Slot(0),
					IsSyncing:    false,
				},
			},
		}

		err := runHealthyTest(t, syncResponseList, 2, false)
		require.NoError(t, err)
	})
}

type syncResponse struct {
	state *v1.SyncState
	delay time.Duration
}

func runHealthyTest(
	t *testing.T,
	syncResponseList []syncResponse,
	syncDistanceTolerance uint64,
	concurrentHealthCheck bool,
) error {
	const (
		commonTimeout = 100 * time.Millisecond
		longTimeout   = 500 * time.Millisecond
	)

	mockResponses := tests.MockResponses()
	replaceSyncing := atomic.Bool{}
	var servers []*httptest.Server
	var urls []string

	for _, syncResp := range syncResponseList {
		mockServer := tests.MockServer(func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
			if r.URL.Path == syncingPath && replaceSyncing.Load() {
				output := struct {
					Data *v1.SyncState `json:"data"`
				}{
					Data: syncResp.state,
				}
				rawData, err := json.Marshal(output)
				if err != nil {
					return nil, err
				}

				if syncResp.delay > 0 {
					time.Sleep(syncResp.delay)
				}

				return rawData, nil
			}

			return mockResponses[r.URL.Path], nil
		})

		servers = append(servers, mockServer)
		urls = append(urls, mockServer.URL)
	}

	c, err := New(t.Context(), zap.NewNop(), Options{
		BeaconNodeAddr:        strings.Join(urls, ";"),
		CommonTimeout:         commonTimeout,
		LongTimeout:           longTimeout,
		SyncDistanceTolerance: syncDistanceTolerance,
	})
	require.NoError(t, err)

	// Multi client library we depend on won't start if client is not synced,
	// so we need to let it start with synced state and then get the state from the test data.
	replaceSyncing.Store(true)

	if !concurrentHealthCheck {
		return c.Healthy(t.Context())
	}

	var wg sync.WaitGroup
	var errMu sync.Mutex
	var errs error
	for range servers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			err := c.Healthy(t.Context())
			if err != nil {
				errMu.Lock()
				errs = errors.Join(errs, err)
				errMu.Unlock()
				return
			}
		}()
	}

	return errs
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
		_, err := New(t.Context(), zap.NewNop(), Options{
			BeaconNodeAddr: undialableServer.URL,
			CommonTimeout:  commonTimeout,
			LongTimeout:    longTimeout,
		})
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
		client, err := New(t.Context(), zap.NewNop(), Options{
			BeaconNodeAddr: unresponsiveServer.URL,
			CommonTimeout:  commonTimeout,
			LongTimeout:    longTimeout,
		})
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
		client, err := New(t.Context(), zap.NewNop(), Options{
			BeaconNodeAddr: unresponsiveServer.URL,
			CommonTimeout:  commonTimeout,
			LongTimeout:    longTimeout,
		})
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
		client, err := New(t.Context(), zap.NewNop(), Options{
			BeaconNodeAddr: fastServer.URL,
			CommonTimeout:  commonTimeout,
			LongTimeout:    longTimeout,
		})
		require.NoError(t, err)

		validators, err := client.GetValidatorData(t.Context(), nil)
		require.NoError(t, err)
		require.NotEmpty(t, validators)

		duties, err := client.ProposerDuties(t.Context(), mockServerEpoch, nil)
		require.NoError(t, err)
		require.NotEmpty(t, duties)
	}
}
