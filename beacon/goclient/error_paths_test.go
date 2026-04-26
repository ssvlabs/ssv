package goclient

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	eth2clienthttp "github.com/attestantio/go-eth2-client/http"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/beacon/goclient/mocks"
)

func Test_specForClient_errorPaths(t *testing.T) {
	t.Parallel()

	tt := []struct {
		name     string
		response mocks.Response
		wantErr  string
		timeout  time.Duration
	}{
		{
			name: "rate limited",
			response: mocks.NewResponse(
				json.RawMessage(`{"message":"rate limited"}`),
				mocks.WithStatusCode(http.StatusTooManyRequests),
				mocks.WithHeader("Retry-After", "1"),
			),
			wantErr: "GET failed with status 429",
			timeout: 100 * time.Millisecond,
		},
		{
			name: "internal server error",
			response: mocks.NewResponse(
				json.RawMessage(`{"message":"boom"}`),
				mocks.WithStatusCode(http.StatusInternalServerError),
			),
			wantErr: "GET failed with status 500",
			timeout: 100 * time.Millisecond,
		},
		{
			name: "timeout",
			response: mocks.NewResponse(
				nil,
				mocks.WithDelay(200*time.Millisecond),
			),
			wantErr: "context deadline exceeded",
			timeout: 50 * time.Millisecond,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := mocks.NewServerWithHandler(func(r *http.Request, resp mocks.Response) (mocks.Response, error) {
				if r.URL.Path == specPath {
					return tc.response, nil
				}
				return resp, nil
			})
			defer server.Close()

			client := &GoClient{log: zap.NewNop()}
			provider := newHTTPService(t, server.URL, tc.timeout)

			_, err := client.specForClient(t.Context(), provider)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func Test_genesisForClient_malformedJSON(t *testing.T) {
	t.Parallel()

	server := mocks.NewServerWithHandler(func(r *http.Request, resp mocks.Response) (mocks.Response, error) {
		if r.URL.Path == genesisPath {
			return mocks.NewResponse(json.RawMessage(`malformed`)), nil
		}
		return resp, nil
	})
	defer server.Close()

	client := &GoClient{log: zap.NewNop()}
	provider := newHTTPService(t, server.URL, 100*time.Millisecond)

	_, err := client.genesisForClient(t.Context(), provider)
	require.ErrorContains(t, err, "invalid character")
}

func newHTTPService(t *testing.T, addr string, timeout time.Duration) *eth2clienthttp.Service {
	t.Helper()

	service, err := eth2clienthttp.New(
		t.Context(),
		eth2clienthttp.WithAddress(addr),
		eth2clienthttp.WithLogLevel(zerolog.Disabled),
		eth2clienthttp.WithTimeout(timeout),
		eth2clienthttp.WithReducedMemoryUsage(true),
		eth2clienthttp.WithAllowDelayedStart(true),
	)
	require.NoError(t, err)

	return service.(*eth2clienthttp.Service)
}
