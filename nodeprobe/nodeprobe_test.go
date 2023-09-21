package nodeprobe

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestProber(t *testing.T) {
	ctx := context.Background()

	node := &node{}
	node.healthy.Store(nil)

	prober := NewProber(zap.L(), nil, map[string]Node{"test node": node})
	prober.interval = 10 * time.Millisecond

	healthy, err := prober.Healthy(ctx)
	require.NoError(t, err)
	require.False(t, healthy)

	prober.Start(ctx)
	prober.Wait()

	healthy, err = prober.Healthy(ctx)
	require.NoError(t, err)
	require.True(t, healthy)

	notHealthy := fmt.Errorf("not healthy")
	node.healthy.Store(&notHealthy)
	time.Sleep(prober.interval * 2)

	healthy, err = prober.Healthy(ctx)
	require.NoError(t, err)
	require.False(t, healthy)
}

func TestProber_UnhealthyHandler(t *testing.T) {
	ctx := context.Background()

	node := &node{}
	node.healthy.Store(nil)

	var unhealthyHandlerCalled atomic.Bool
	unhealthyHandler := func() {
		unhealthyHandlerCalled.Store(true)
	}
	prober := NewProber(zap.L(), unhealthyHandler, map[string]Node{"test node": node})
	prober.interval = 10 * time.Millisecond
	prober.Start(ctx)
	prober.Wait()

	healthy, err := prober.Healthy(ctx)
	require.NoError(t, err)
	require.True(t, healthy)

	notHealthy := fmt.Errorf("not healthy")
	node.healthy.Store(&notHealthy)

	time.Sleep(prober.interval * 2)
	require.True(t, unhealthyHandlerCalled.Load())

	healthy, err = prober.Healthy(ctx)
	require.NoError(t, err)
	require.False(t, healthy)
}

type node struct {
	healthy atomic.Pointer[error]
}

func (sc *node) Healthy(context.Context) error {
	err := sc.healthy.Load()
	if err != nil {
		return *err
	}
	return nil
}
