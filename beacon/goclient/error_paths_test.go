package goclient

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	eth2api "github.com/attestantio/go-eth2-client/api"
	eth2clienthttp "github.com/attestantio/go-eth2-client/http"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/beacon/goclient/mocks"
)

func Test_specForClient_errorPaths(t *testing.T) {
	t.Parallel()

	tt := []struct {
		name           string
		response       mocks.Response
		wantStatusCode int
		requestTimeout time.Duration
	}{
		{
			name: "rate limited",
			response: mocks.NewResponse(
				json.RawMessage(`{"message":"rate limited"}`),
				mocks.WithStatusCode(http.StatusTooManyRequests),
				mocks.WithHeader("Retry-After", "1"),
			),
			wantStatusCode: http.StatusTooManyRequests,
		},
		{
			name: "service unavailable",
			response: mocks.NewResponse(
				json.RawMessage(`{"message":"temporarily unavailable"}`),
				mocks.WithStatusCode(http.StatusServiceUnavailable),
			),
			wantStatusCode: http.StatusServiceUnavailable,
		},
		{
			name: "internal server error",
			response: mocks.NewResponse(
				json.RawMessage(`{"message":"boom"}`),
				mocks.WithStatusCode(http.StatusInternalServerError),
			),
			wantStatusCode: http.StatusInternalServerError,
		},
		{
			name: "timeout",
			response: mocks.NewResponse(
				nil,
				mocks.WithDelay(400*time.Millisecond),
			),
			wantStatusCode: 0,
			requestTimeout: 50 * time.Millisecond,
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
			provider := newHTTPService(t, server.URL)

			ctx := t.Context()
			if tc.requestTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.requestTimeout)
				defer cancel()
			}

			_, err := client.specForClient(ctx, provider)
			if tc.wantStatusCode == 0 {
				require.ErrorIs(t, err, context.DeadlineExceeded)
				return
			}

			assertAPIErrorStatusCode(t, err, tc.wantStatusCode)
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
	provider := newHTTPService(t, server.URL)

	_, err := client.genesisForClient(t.Context(), provider)
	require.ErrorContains(t, err, "invalid character")
}

func newHTTPService(t *testing.T, addr string) *eth2clienthttp.Service {
	t.Helper()

	service, err := eth2clienthttp.New(
		t.Context(),
		eth2clienthttp.WithAddress(addr),
		eth2clienthttp.WithLogLevel(zerolog.Disabled),
		eth2clienthttp.WithTimeout(time.Second),
		// Reduced memory usage skips eager spec/genesis fetches, which keeps the
		// cache cold so these tests exercise the mocked HTTP responses directly.
		eth2clienthttp.WithReducedMemoryUsage(true),
	)
	require.NoError(t, err)

	provider := service.(*eth2clienthttp.Service)
	require.Eventually(t, provider.IsActive, time.Second, 10*time.Millisecond)

	return provider
}

func assertAPIErrorStatusCode(t *testing.T, err error, wantStatusCode int) {
	t.Helper()

	var apiErr *eth2api.Error
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, wantStatusCode, apiErr.StatusCode)
}
