package executionclient

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/observability/log"
)

func TestRememberBatchingUnsupported(t *testing.T) {
	newClient := func() *ethClient { return &ethClient{logger: log.TestLogger(t)} }

	t.Run("latches on a substantive wholesale rejection", func(t *testing.T) {
		c := newClient()
		c.rememberBatchingUnsupported(true, errors.New("batch requests are not supported"))
		require.True(t, c.batchingUnsupported.Load())
	})

	t.Run("does not latch on a batch timeout", func(t *testing.T) {
		c := newClient()
		// A slow-but-batching-capable provider whose batch hit the per-request timeout while the
		// sequential fallback still succeeded must not be permanently downgraded to sequential.
		c.rememberBatchingUnsupported(true, context.DeadlineExceeded)
		require.False(t, c.batchingUnsupported.Load())
		c.rememberBatchingUnsupported(true, fmt.Errorf("batch call: %w", context.DeadlineExceeded))
		require.False(t, c.batchingUnsupported.Load())
	})

	t.Run("does not latch on cancellation", func(t *testing.T) {
		c := newClient()
		c.rememberBatchingUnsupported(true, context.Canceled)
		require.False(t, c.batchingUnsupported.Load())
	})

	t.Run("does not latch without an attempted, failed batch", func(t *testing.T) {
		c := newClient()
		c.rememberBatchingUnsupported(false, errors.New("boom")) // no batch attempted
		require.False(t, c.batchingUnsupported.Load())
		c.rememberBatchingUnsupported(true, nil) // batch succeeded
		require.False(t, c.batchingUnsupported.Load())
	})
}
