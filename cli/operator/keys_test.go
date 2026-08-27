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
			context.Background(), zap.NewNop(), client, localKeys, 50*time.Millisecond, 10*time.Millisecond)
		require.ErrorContains(t, err, "unexpected status: 500")
		require.Greater(t, hits, 1, "expected at least one retry within the window")
	})

	t.Run("stops retrying when ctx is canceled", func(t *testing.T) {
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
}
