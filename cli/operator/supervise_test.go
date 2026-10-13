package operator

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// superviseTestGrace is generous enough that the graceful-teardown window never fires for the
// well-behaved cases below (whose teardown is ~instant), so those assert the cause, not the timeout.
const superviseTestGrace = 5 * time.Second

// Test_supervise_startFailureBecomesCause: a start failure is a terminal event — supervise returns
// it as the cause and still runs teardown.
func Test_supervise_startFailureBecomesCause(t *testing.T) {
	errBoom := errors.New("start boom")
	var torn atomic.Bool

	err := supervise(context.Background(), zap.NewNop(), superviseTestGrace,
		func(context.Context, func(func() error)) error { return errBoom },
		func() error { torn.Store(true); return nil })

	require.ErrorIs(t, err, errBoom)
	require.True(t, torn.Load(), "teardown must run even when start fails")
}

// Test_supervise_serviceFailureBecomesCause: a long-lived service that fails cancels the root with
// its error as the cause, which supervise returns after tearing down.
func Test_supervise_serviceFailureBecomesCause(t *testing.T) {
	errBoom := errors.New("service boom")
	var torn atomic.Bool

	err := supervise(context.Background(), zap.NewNop(), superviseTestGrace,
		func(_ context.Context, spawn func(func() error)) error {
			spawn(func() error { return errBoom })
			return nil
		},
		func() error { torn.Store(true); return nil })

	require.ErrorIs(t, err, errBoom)
	require.True(t, torn.Load(), "teardown must run on the way out")
}

// Test_supervise_signalCancelSurfacesAsCanceled: a deliberate stop (the parent ctx canceled, as a
// shutdown signal does) surfaces as context.Canceled — not a failure — while still tearing down.
func Test_supervise_signalCancelSurfacesAsCanceled(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	var torn atomic.Bool

	go cancelParent() // the "signal"

	err := supervise(parent, zap.NewNop(), superviseTestGrace,
		func(ctx context.Context, spawn func(func() error)) error {
			// A well-behaved long-lived service: stays up until the root is canceled, then returns nil.
			spawn(func() error { <-ctx.Done(); return nil })
			return nil
		},
		func() error { torn.Store(true); return nil })

	require.ErrorIs(t, err, context.Canceled)
	require.True(t, torn.Load(), "teardown must run on a deliberate stop")
}

// Test_supervise_wedgedServiceHitsGraceTimeout: a service that ignores cancellation must not hang
// teardown — past the grace window supervise gives up and returns the cause wrapped in a timeout.
func Test_supervise_wedgedServiceHitsGraceTimeout(t *testing.T) {
	errBoom := errors.New("trigger boom")
	release := make(chan struct{})
	defer close(release) // let the wedged goroutine exit at test end rather than leak permanently

	err := supervise(context.Background(), zap.NewNop(), 50*time.Millisecond,
		func(_ context.Context, spawn func(func() error)) error {
			spawn(func() error { <-release; return nil }) // wedged: ignores ctx
			spawn(func() error { return errBoom })        // fails -> terminal event
			return nil
		},
		func() error { return nil })

	require.ErrorContains(t, err, "graceful shutdown timed out")
	require.ErrorIs(t, err, errBoom, "the timeout error must still wrap the terminal cause")
}
