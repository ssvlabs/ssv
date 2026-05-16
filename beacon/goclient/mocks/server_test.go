package mocks

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewServerWithHandler(t *testing.T) {
	t.Run("unexpected requests return not found", func(t *testing.T) {
		server := NewServer(nil)
		defer server.Close()

		resp, err := http.Get(server.URL + "/unexpected")
		require.NoError(t, err)
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		require.Equal(t, http.StatusNotFound, resp.StatusCode)
		require.JSONEq(t, `{"error":"unexpected request","path":"/unexpected"}`, string(body))
	})

	t.Run("custom responses support status headers and delay", func(t *testing.T) {
		server := NewServerWithHandler(func(r *http.Request, resp Response) (Response, error) {
			if r.URL.Path == "/eth/v1/beacon/genesis" {
				return NewResponse(
					json.RawMessage(`malformed`),
					WithStatusCode(http.StatusTooManyRequests),
					WithHeader("Retry-After", "1"),
					WithDelay(25*time.Millisecond),
				), nil
			}

			return resp, nil
		})
		defer server.Close()

		start := time.Now()
		resp, err := http.Get(server.URL + "/eth/v1/beacon/genesis")
		require.NoError(t, err)
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		require.GreaterOrEqual(t, time.Since(start), 25*time.Millisecond)
		require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
		require.Equal(t, "1", resp.Header.Get("Retry-After"))
		require.Equal(t, "malformed", string(body))
	})

	t.Run("callback errors return internal server error", func(t *testing.T) {
		server := NewServerWithHandler(func(r *http.Request, resp Response) (Response, error) {
			return Response{}, errors.New("boom")
		})
		defer server.Close()

		resp, err := http.Get(server.URL + "/eth/v1/config/spec")
		require.NoError(t, err)
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		require.Contains(t, string(body), "onRequestFn returned error: boom")
	})
}
