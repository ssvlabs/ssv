package operator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/ssvlabs/ssv/exporter"
)

func TestShouldRunDutyScheduler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		exporterOpts exporter.Options
		expected     bool
	}{
		{
			name:         "regular operator",
			exporterOpts: exporter.Options{},
			expected:     true,
		},
		{
			name: "exporter standard",
			exporterOpts: exporter.Options{
				Enabled: true,
				Mode:    exporter.ModeStandard,
			},
			expected: false,
		},
		{
			name: "exporter archive",
			exporterOpts: exporter.Options{
				Enabled: true,
				Mode:    exporter.ModeArchive,
			},
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.expected, shouldRunDutyScheduler(tc.exporterOpts))
		})
	}
}

// TestNode_runServices_returnsNilOnCtxCancel locks in the normal-shutdown invariant for the
// operator-package side: with the duty scheduler absent (exporter-standard) and no WS server, the
// joined members must return nil on a plain ctx cancellation, so a clean shutdown unwinds through
// Start as nil rather than a spurious error.
func TestNode_runServices_returnsNilOnCtxCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	g, gctx := errgroup.WithContext(ctx)

	n := &Node{
		logger:          zap.NewNop(),
		exporterOptions: exporter.Options{Enabled: true, Mode: exporter.ModeStandard},
	}

	done := make(chan error, 1)
	go func() { done <- n.runServices(g, gctx, nil) }()

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err, "runServices must return nil on a clean ctx cancellation")
	case <-time.After(5 * time.Second):
		t.Fatal("runServices did not return after ctx cancellation")
	}
}

// TestNode_runServices_propagatesWSServeError verifies a WS serve-loop failure is surfaced as a
// returned error (not a Fatal) and cancels the group so the other members unwind. The select-on-gctx
// join also guards against deadlock when the WS server's serveErr is tied to a different ctx.
func TestNode_runServices_propagatesWSServeError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g, gctx := errgroup.WithContext(ctx)

	wsServeErr := make(chan error, 1)
	wsServeErr <- errors.New("boom")

	// Exporter-standard so the scheduler member blocks on gctx and unwinds cleanly once the WS
	// failure cancels the group.
	n := &Node{
		logger:          zap.NewNop(),
		exporterOptions: exporter.Options{Enabled: true, Mode: exporter.ModeStandard},
	}

	err := n.runServices(g, gctx, wsServeErr)
	require.ErrorContains(t, err, "WS server serve loop exited")
}

// TestNode_runServices_schedulerNilNonExporterStandard verifies the wiring-invariant check: a node
// without a duty scheduler that is not exporter-standard surfaces an error rather than idling
// silently (previously a Fatal).
func TestNode_runServices_schedulerNilNonExporterStandard(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g, gctx := errgroup.WithContext(ctx)

	// Operator mode (exporter disabled) with a nil dutyScheduler is the inconsistent state the check
	// guards.
	n := &Node{
		logger:          zap.NewNop(),
		exporterOptions: exporter.Options{},
	}

	err := n.runServices(g, gctx, nil)
	require.ErrorContains(t, err, "duty scheduler is nil")
}
