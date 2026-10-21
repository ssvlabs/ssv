package goclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGloasPublishSSZ_Non2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad block"))
	}))
	defer srv.Close()

	err := gloasPublishSSZ(context.Background(), unmaskedAddr{srv.URL}, gloasPublishBlockPath, []byte{0x01}, nil)
	require.ErrorContains(t, err, "status 400")
}

// TestHTTPDoErrorsOmitBeaconAddr pins that a failed hand-rolled request names only its route: the unmasked
// beacon address it is sent to may carry basic-auth credentials or an API key in its path.
func TestHTTPDoErrorsOmitBeaconAddr(t *testing.T) {
	const password, apiKey = "s3cret-password", "s3cret-api-key"

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()
	unreachable := httptest.NewServer(http.NotFoundHandler())
	unreachable.Close()

	withSecrets := func(base string) string {
		u, err := url.Parse(base)
		require.NoError(t, err)
		u.User = url.UserPassword("user", password)
		u.Path = "/rpc/" + apiKey
		return u.String()
	}

	for name, addr := range map[string]string{
		"non-2xx response":  withSecrets(failing.URL),
		"transport error":   withSecrets(unreachable.URL),
		"malformed address": "http://user:" + password + "@host:port/rpc/" + apiKey,
	} {
		t.Run(name, func(t *testing.T) {
			_, _, _, err := httpDo(t.Context(), http.DefaultClient, http.MethodPost, unmaskedAddr{addr}, payloadAttestationsPath, nil, "application/json", "", nil)
			require.ErrorContains(t, err, http.MethodPost+" "+payloadAttestationsPath)
			require.NotContains(t, err.Error(), password)
			require.NotContains(t, err.Error(), apiKey)
		})
	}
}

// TestUnmaskedAddrString pins that formatting an unmaskedAddr masks its credentials, while UnmaskedAddress keeps
// them for the request URL.
func TestUnmaskedAddrString(t *testing.T) {
	const raw = "http://user:s3cret-password@host:5052/rpc/s3cret-api-key"
	addr := unmaskedAddr{raw}
	require.Equal(t, raw, addr.UnmaskedAddress())
	require.Equal(t, "http://user:xxxxx@host:5052/xxxxx", fmt.Sprint(addr))
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
