package operator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// service is a long-lived unit of work run by supervise: it runs in its own goroutine, and the first
// service to return a non-nil error becomes the terminal cause that unwinds the rest.
type service = func() error

// supervise runs a lifecycle and blocks until it ends: start launches the long-lived services through
// spawn, and supervise waits until the first spawned service returns an error, start itself fails, or
// the parent ctx is canceled (a shutdown signal → context.Canceled) — whichever comes first. That
// cause then tears everything down within graceTimeout (awaiting the still-running services, then
// teardown) and is returned. start returns once the services are up; what's supervised is the
// services it spawned. Past the grace window supervise surfaces the cause rather than hang forever on
// something ignoring cancellation (the leftovers are reaped at process exit).
func supervise(
	ctx context.Context,
	logger *zap.Logger,
	graceTimeout time.Duration,
	start func(ctx context.Context, spawn func(service)) error,
	teardown func() error,
) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	var wg sync.WaitGroup
	spawn := func(svc service) {
		wg.Go(func() {
			if err := svc(); err != nil {
				cancel(err)
			}
		})
	}
	if err := start(ctx, spawn); err != nil {
		cancel(err) // a start failure is a terminal event too (the first cause wins)
	}

	<-ctx.Done()
	cause := context.Cause(ctx)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		if err := teardown(); err != nil {
			logger.Error("graceful teardown reported an error", zap.Error(err))
		}
		close(done)
	}()
	select {
	case <-done:
		return cause
	case <-time.After(graceTimeout):
		return fmt.Errorf("graceful shutdown timed out after %s: %w", graceTimeout, cause)
	}
}
