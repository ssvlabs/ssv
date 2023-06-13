package nodeprobe

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestProber(t *testing.T) {
	ctx := context.Background()

	checker := &statusChecker{
		ready: true,
	}

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

	checker.ready = false
	time.Sleep(2 * time.Second)

	ready, err = prober.IsReady(ctx)
	require.False(t, ready)
}

type statusChecker struct {
	ready bool
}

func (sc *statusChecker) IsReady(context.Context) (bool, error) {
	return sc.ready, nil
}
