package twoab

import (
	"context"
	"time"
)

// sleepAwareTimeoutCtx returns a context that cancels after `budget` of
// ACTIVE wall time. A companion goroutine ticks at a known cadence; any
// individual gap between ticks beyond suspendThreshold is treated as a
// process suspend (laptop sleep, debugger pause, OS freeze) and credited
// back to the deadline. The test therefore survives sleep cycles
// regardless of how the host platform's monotonic clock behaves during
// suspend.
//
// Why platform-independent: macOS's monotonic source (mach_continuous_time
// in Go 1.18+) advances during sleep, so context.WithTimeout consumes the
// budget while the laptop is asleep — by wake time, the test deadline has
// fired and the resolve loop bails before it can drain any late-arriving
// wire messages. Linux's CLOCK_MONOTONIC is typically suspend-aware
// (doesn't advance), so context.WithTimeout would survive sleep "for
// free" there. This helper does the right thing on either platform: by
// comparing actual vs requested sleep durations purely in Go (no
// clock-source assumption), it detects suspends regardless of OS
// behavior.
//
// Tuning: tick=200ms gives prompt detection of suspends > suspendThreshold
// (2s). 2s is well above realistic Go-scheduler jitter under -race ×
// heavy GOMAXPROCS (sub-100ms in practice) and well below any
// laptop-sleep cycle worth catching.
func sleepAwareTimeoutCtx(parent context.Context, budget time.Duration) (context.Context, context.CancelFunc) {
	const (
		tick             = 200 * time.Millisecond
		suspendThreshold = 2 * time.Second
	)
	ctx, innerCancel := context.WithCancel(parent)
	deadline := time.Now().Add(budget)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			before := time.Now()
			select {
			case <-ctx.Done():
				return
			case <-time.After(tick):
			}
			gap := time.Since(before)
			if gap > suspendThreshold {
				// Process was suspended for ~(gap - tick). Credit it back to
				// the deadline so the budget is consumed only by active time.
				deadline = deadline.Add(gap - tick)
			}
			if !time.Now().Before(deadline) {
				innerCancel()
				return
			}
		}
	}()
	return ctx, func() { innerCancel(); <-done }
}
