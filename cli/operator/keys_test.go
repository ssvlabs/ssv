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
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

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
			context.Background(), zap.NewNop(), client, localKeys,
			retryBackoff{window: time.Minute, delay: time.Millisecond, maxDelay: 16 * time.Millisecond})
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
			context.Background(), zap.NewNop(), client, localKeys,
			retryBackoff{window: 200 * time.Millisecond, delay: 20 * time.Millisecond, maxDelay: time.Second})
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
			ctx, zap.NewNop(), client, localKeys,
			retryBackoff{window: time.Hour, delay: time.Hour, maxDelay: time.Hour})
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
				ctx, zap.NewNop(), client, localKeys,
				retryBackoff{window: time.Hour, delay: 10 * time.Second, maxDelay: time.Minute})
			done <- err
		}()

		select {
		case err := <-done:
			require.Error(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("expected the backoff wait to be interrupted by ctx cancellation")
		}
	})

	// Drive several retries against a small injected cap and assert the logged
	// retry_in delays double from the initial value and then plateau at the cap.
	t.Run("caps the backoff delay at maxDelay", func(t *testing.T) {
		const stopAfter = 6 // enough retries to reach the cap and stay there

		core, logs := observer.New(zapcore.DebugLevel)
		ctx, cancel := context.WithCancel(context.Background())

		var hits int
		client := newTestSSVSignerClient(t, func(mux *http.ServeMux) {
			mux.HandleFunc(ssvsigner.PathValidators, func(w http.ResponseWriter, r *http.Request) {
				hits++
				if hits >= stopAfter {
					cancel() // stop retrying once enough backoffs are logged
				}
				http.Error(w, "web3signer starting up", http.StatusInternalServerError)
			})
		})

		initialDelay, maxDelay := time.Millisecond, 4*time.Millisecond
		_, err := fetchMissingKeysWithRetry(
			ctx, zap.New(core), client, localKeys,
			retryBackoff{window: time.Hour, delay: initialDelay, maxDelay: maxDelay})
		require.Error(t, err)

		var delays []time.Duration
		for _, entry := range logs.All() {
			d, ok := entry.ContextMap()["retry_in"].(time.Duration)
			require.True(t, ok, "retry log is missing a retry_in duration")
			delays = append(delays, d)
		}
		require.GreaterOrEqual(t, len(delays), 4, "need enough retries to observe the plateau")

		want := initialDelay
		for i, got := range delays {
			require.Equal(t, want, got, "unexpected backoff at retry %d", i)
			want = min(want*2, maxDelay)
		}
		require.Equal(t, maxDelay, delays[len(delays)-1], "backoff should plateau at the cap")
	})
}
