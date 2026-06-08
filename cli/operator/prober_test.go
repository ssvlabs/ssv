package operator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/hprobe"
)

// wedgedComponent is a hprobe component that never responds: its Healthy blocks until the
// health-check ctx times out, surfacing as DeadlineExceeded — the realistic "hung EL/CL" case the
// watchdog must catch.
type wedgedComponent struct{}

func (wedgedComponent) Healthy(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

// Test_startHealthProber_tripsOnUnhealthyComponent verifies the watchdog returns the unhealth error
// (so the node terminates non-zero and the orchestrator restarts it) when a component is persistently
// unhealthy while the ctx is still live — here a wedged component whose failure surfaces as a probe
// DeadlineExceeded. This is the liveness property the watchdog exists for, and it must trip off the
// parent ctx state rather than the error value (a DeadlineExceeded must not be mistaken for a cancel).
func Test_startHealthProber_tripsOnUnhealthyComponent(t *testing.T) {
	p := hprobe.NewHealthProber(zap.NewNop())
	// Wedged component: blocks until its 20ms health-check ctx times out -> DeadlineExceeded. No
	// retries, so the watchdog trips on the first tick.
	p.AddComponent("el", wedgedComponent{}, 20*time.Millisecond, 0, 0)

	err := startHealthProber(context.Background(), zap.NewNop(), p)
	require.ErrorContains(t, err, componentsUnhealthyErrorMsg)
}

// Test_startHealthProber_returnsNilOnCtxCancel verifies a clean ctx cancellation (normal shutdown)
// returns nil rather than tripping the watchdog.
func Test_startHealthProber_returnsNilOnCtxCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := hprobe.NewHealthProber(zap.NewNop()) // no components -> ProbeAll is a no-op

	done := make(chan error, 1)
	go func() { done <- startHealthProber(ctx, zap.NewNop(), p) }()

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err, "startHealthProber must return nil on a clean ctx cancellation")
	case <-time.After(5 * time.Second):
		t.Fatal("startHealthProber did not return after ctx cancellation")
	}
}
