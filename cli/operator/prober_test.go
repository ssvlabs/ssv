package operator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/hprobe"
)

// simpleComponent implements hprobe's component interface for testing.
type simpleComponent struct{ err error }

func (s simpleComponent) Healthy(context.Context) error { return s.err }

func Test_startHealthProber_returnsNilOnCtxCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	prober := hprobe.NewHealthProber(zap.NewNop())
	prober.AddComponent("cl", simpleComponent{}, time.Second, 0, 0)

	done := make(chan error, 1)
	go func() { done <- startHealthProber(ctx, zap.NewNop(), prober) }()

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("startHealthProber did not exit after ctx cancellation")
	}
}

func Test_startHealthProber_returnsErrOnUnhealthy(t *testing.T) {
	prober := hprobe.NewHealthProber(zap.NewNop())
	prober.AddComponent("cl", simpleComponent{err: errors.New("broken")}, 100*time.Millisecond, 0, 0)

	err := startHealthProber(context.Background(), zap.NewNop(), prober)
	require.Error(t, err)
	require.ErrorContains(t, err, componentsUnhealthyErrorMsg)
}

// Test_startHealthProber_ctxCancelMasksProbeFail verifies the ctx.Err() guard: when ctx is
// cancelled at the same time a probe fails (e.g. the network stack tears down components before
// the errgroup context fully propagates), startHealthProber returns nil rather than a misleading
// "components unhealthy" error that would cause a Fatal log on normal shutdown.
func Test_startHealthProber_ctxCancelMasksProbeFail(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before starting — simulates shutdown racing a probe

	prober := hprobe.NewHealthProber(zap.NewNop())
	prober.AddComponent("cl", simpleComponent{err: errors.New("broken")}, 100*time.Millisecond, 0, 0)

	err := startHealthProber(ctx, zap.NewNop(), prober)
	require.NoError(t, err)
}
