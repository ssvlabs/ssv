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
	prober.interval = 1 * time.Millisecond

	ready, err := prober.Healthy(ctx)
	require.NoError(t, err)
	require.False(t, ready)

	prober.Start(ctx)
	prober.Wait()

	ready, err = prober.Healthy(ctx)
	require.NoError(t, err)
	require.True(t, ready)

	notHealthy := fmt.Errorf("not healthy")
	node.healthy.Store(&notHealthy)
	time.Sleep(prober.interval * 2)

	ready, err = prober.Healthy(ctx)
	require.NoError(t, err)
	require.False(t, ready)

	var unreadyHandlerCalled atomic.Bool
	unreadyHandler := func() {
		unreadyHandlerCalled.Store(true)
	}
	prober = NewProber(zap.L(), unreadyHandler, map[string]Node{"test node": node})
	prober.Start(ctx)
	prober.Wait()

	time.Sleep(prober.interval * 2)
	require.True(t, unreadyHandlerCalled.Load())
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
