package nodeprobe

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestProber(t *testing.T) {
	ctx := context.Background()

	checker := &statusChecker{}
	checker.ready.Store(true)

	prober := NewProber(zap.L(), checker)
	prober.interval = 1 * time.Millisecond

	ready, err := prober.IsReady(ctx)
	require.NoError(t, err)
	require.False(t, ready)

	prober.Start(ctx)
	prober.Wait()

	ready, err = prober.IsReady(ctx)
	require.NoError(t, err)
	require.True(t, ready)

	checker.ready.Store(false)
	time.Sleep(2 * time.Second)

	ready, err = prober.IsReady(ctx)
	require.NoError(t, err)
	require.False(t, ready)
}

type statusChecker struct {
	ready atomic.Bool
}

func (sc *statusChecker) IsReady(context.Context) (bool, error) {
	return sc.ready.Load(), nil
}
