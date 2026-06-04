package metrics

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type stubHealthChecker struct{}

func (stubHealthChecker) HealthCheck() error { return nil }

// TestHandler_Start verifies the metrics server binds on the given address, serves in the
// background, and shuts down cleanly when its ctx is canceled (closing serveErr without
// emitting a non-graceful error).
func TestHandler_Start(t *testing.T) {
	h := NewHandler(zap.NewNop(), nil, false, stubHealthChecker{})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	addr, serveErr, err := h.Start(ctx, http.NewServeMux(), "127.0.0.1:0")
	require.NoError(t, err)
	require.NotEmpty(t, addr)

	// The server is up and serving the Prometheus endpoint.
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + addr + "/metrics")
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp.Body)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Canceling ctx triggers graceful shutdown: Serve returns ErrServerClosed (filtered out),
	// so serveErr is closed rather than receiving a value.
	cancel()
	select {
	case err, ok := <-serveErr:
		require.Falsef(t, ok, "expected serveErr to close on graceful shutdown, got error: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("metrics server did not shut down within 10s of ctx cancel")
	}
}
