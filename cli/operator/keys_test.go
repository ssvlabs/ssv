package operator

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner"
)

func Test_fetchMissingKeysWithRetry(t *testing.T) {
	localKeys := []phase0.BLSPubKey{{1, 2, 3}}

	t.Run("succeeds after transient failures", func(t *testing.T) {
		var hits int
		client := newTestSSVSignerClient(t, func(mux *http.ServeMux) {
			mux.HandleFunc(ssvsigner.PathValidators, func(w http.ResponseWriter, r *http.Request) {
				hits++
				if hits <= 2 {
					http.Error(w, "web3signer starting up", http.StatusInternalServerError)
					return
				}
				require.NoError(t, json.NewEncoder(w).Encode(localKeys))
			})
		})

		missingKeys, err := fetchMissingKeysWithRetry(
			context.Background(), zap.NewNop(), client, localKeys, time.Minute, time.Millisecond)
		require.NoError(t, err)
		require.Empty(t, missingKeys)
		require.Equal(t, 3, hits)
	})

	t.Run("gives up when the window elapses", func(t *testing.T) {
		var hits int
		client := newTestSSVSignerClient(t, func(mux *http.ServeMux) {
			mux.HandleFunc(ssvsigner.PathValidators, func(w http.ResponseWriter, r *http.Request) {
				hits++
				http.Error(w, "permanent failure", http.StatusInternalServerError)
			})
		})

		_, err := fetchMissingKeysWithRetry(
			context.Background(), zap.NewNop(), client, localKeys, 200*time.Millisecond, 20*time.Millisecond)
		require.ErrorContains(t, err, "unexpected status: 500")
		require.Greater(t, hits, 1, "expected at least one retry within the window")
	})

	// Cancellation during a request: the helper bails at the ctx.Err() check,
	// before reaching any backoff wait.
	t.Run("stops retrying when ctx is canceled during a request", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		client := newTestSSVSignerClient(t, func(mux *http.ServeMux) {
			mux.HandleFunc(ssvsigner.PathValidators, func(w http.ResponseWriter, r *http.Request) {
				cancel()
				http.Error(w, "failure", http.StatusInternalServerError)
			})
		})

		start := time.Now()
		_, err := fetchMissingKeysWithRetry(
			ctx, zap.NewNop(), client, localKeys, time.Hour, time.Hour)
		require.Error(t, err)
		require.Less(t, time.Since(start), time.Minute, "expected to give up without waiting out the backoff")
	})

	// Cancellation during the backoff wait (the select), not a request — the path
	// that keeps shutdown prompt when a signal lands mid-backoff.
	t.Run("aborts the backoff wait when ctx is canceled between attempts", func(t *testing.T) {
		client := newTestSSVSignerClient(t, func(mux *http.ServeMux) {
			mux.HandleFunc(ssvsigner.PathValidators, func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "failure", http.StatusInternalServerError)
			})
		})

		ctx, cancel := context.WithCancel(context.Background())
		// First attempt fails fast; cancel while the retry is parked on its long
		// backoff, so cancellation interrupts the wait itself.
		go func() {
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()

		done := make(chan error, 1)
		go func() {
			_, err := fetchMissingKeysWithRetry(
				ctx, zap.NewNop(), client, localKeys, time.Hour, 10*time.Second)
			done <- err
		}()

		select {
		case err := <-done:
			require.Error(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("expected the backoff wait to be interrupted by ctx cancellation")
		}
	})
}
