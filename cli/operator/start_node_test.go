package operator

import (
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Test_handleShutdownSignals_firstStopsGracefullySecondForces pins the two-stage behavior: the first
// signal triggers the graceful stop (and nothing else), a second one triggers the force-exit hatch.
func Test_handleShutdownSignals_firstStopsGracefullySecondForces(t *testing.T) {
	sigC := make(chan os.Signal, 2)
	var gracefulStopped atomic.Bool
	forced := make(chan os.Signal, 1)

	go handleShutdownSignals(sigC, zap.NewNop(),
		func() { gracefulStopped.Store(true) },
		func(sig os.Signal) { forced <- sig },
	)

	// First signal: graceful stop fires, force-exit does not.
	sigC <- syscall.SIGTERM
	require.Eventually(t, gracefulStopped.Load, time.Second, 5*time.Millisecond,
		"first signal must trigger the graceful stop")
	select {
	case <-forced:
		t.Fatal("force-exit must not fire before a second signal")
	default:
	}

	// Second signal: force-exit fires, carrying that signal.
	sigC <- syscall.SIGINT
	select {
	case sig := <-forced:
		require.Equal(t, syscall.SIGINT, sig)
	case <-time.After(time.Second):
		t.Fatal("second signal must trigger the force-exit")
	}
}
