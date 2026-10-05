package goclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// With no beacon clients, firstClientResult fails instead of returning the zero result as a success.
func TestFirstClientResult_NoClients(t *testing.T) {
	called := false
	_, err := firstClientResult(context.Background(), &GoClient{}, "Route", http.MethodGet, func(context.Context, string) (int, error) {
		called = true
		return 1, nil
	})
	require.ErrorContains(t, err, "no clients available")
	require.False(t, called)
}

func TestGloasPublishSSZ_Non2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad block"))
	}))
	defer srv.Close()

	err := gloasPublishSSZ(context.Background(), srv.URL, []byte{0x01}, nil)
	require.ErrorContains(t, err, "status 400")
}

func TestIsAlreadyKnown(t *testing.T) {
	require.False(t, isAlreadyKnown(nil))
	require.False(t, isAlreadyKnown(errors.New("some other error")))
	require.False(t, isAlreadyKnown(&httpStatusError{status: http.StatusBadRequest, body: "invalid block"}))
	// Lighthouse's equivocation rejection: a conflicting block, not a repeat, so it must stay an error.
	require.False(t, isAlreadyKnown(&httpStatusError{status: http.StatusBadRequest, body: `{"code":400,"message":"BAD_REQUEST: proposal for this slot and proposer has already been seen","stacktraces":[]}`}))
	require.True(t, isAlreadyKnown(&httpStatusError{status: http.StatusInternalServerError, body: `{"message":"BLOCK_ERROR_ALREADY_KNOWN"}`}))
	require.True(t, isAlreadyKnown(&httpStatusError{status: http.StatusInternalServerError, body: `{"message":"EXECUTION_PAYLOAD_ENVELOPE_ERROR_ALREADY_KNOWN"}`}))
	require.True(t, isAlreadyKnown(&httpStatusError{status: http.StatusBadRequest, body: "block already known"}))
	// Lighthouse with --http-duplicate-block-status set to a non-2xx.
	require.True(t, isAlreadyKnown(&httpStatusError{status: http.StatusConflict, body: `{"code":409,"message":"duplicate block","stacktraces":[]}`}))
}
