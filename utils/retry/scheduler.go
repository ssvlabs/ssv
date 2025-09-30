package retry

import (
	"math/rand"
	"sync"
	"time"
)

// timer abstracts requirement for timers; used for testing.
type timer interface {
	Stop() bool
}

// BackoffConfig configures the retry backoff policy.
type BackoffConfig struct {
	// Initial is the initial duration to wait before the first retry attempt.
	Initial time.Duration
	// Max is the maximum duration to wait between retry attempts.
	// Backoff durations grow exponentially up to this maximum.
	// If zero, defaults to Initial.
	// Note: the actual delay may be longer due to jitter.
	Max time.Duration
	// Jitter is the maximum random jitter to add to each backoff duration.
	// If zero, no jitter is added.
	// Jitter is drawn uniformly from [0, Jitter).
	// Example: with Initial=1s and Jitter=500ms, the first backoff will be
	// in the range [1s, 1.5s).
	Jitter time.Duration
}

// Scheduler runs keyed retry loops with exponential backoff.
// For each key there is at most one pending attempt at a time.
//
// Usage:
//
//	Schedule(key, fn) schedules/continues the retry loop for that key.
//	fn receives a 1-based attempt count and returns either
//	true on success to stop the loop or false to retry after the next backoff.
//
// Semantics:
//   - Per-key serialization: only one attempt is in-flight per key.
//     Calling Schedule(key, …) replaces any pending timer for that key.
//   - Stop(key): cancel the pending timer but keep the attempt counter
//     (pauses retries without resetting backoff).
//   - Cancel(key): cancel the pending timer and clear the counter
//     (next Schedule restarts from the initial backoff).
//   - Close(): cancel all timers, prevent new schedules, and close Done().
//
// Notes:
//
//	– This is not a worker pool; timers trigger attempts asynchronously.
//	– Methods are safe for concurrent use.
type Scheduler struct {
	// itemsMutex serializes access to per-key scheduling state. It protects the
	// items map and the lifecycle of timers and attempt counters across
	// Schedule/Stop/Cancel/Close so there is at most one active timer per key
	// and updates are atomic with respect to one another.
	itemsMutex sync.Mutex
	items      map[string]scheduledAttempt
	cfg        BackoffConfig
	afterFunc  func(d time.Duration, f func()) timer
	done       chan struct{}
	closeOnce  sync.Once
}

// scheduledAttempt tracks per-key state: attempt counter and the active timer.
type scheduledAttempt struct {
	attempt int
	t       timer
}

// clearTimer halts the timer if set and clears it.
func (it *scheduledAttempt) clearTimer() {
	if it.t != nil {
		it.t.Stop()
		it.t = nil
	}
}

// NewScheduler creates a new Scheduler with the given config.
func NewScheduler(cfg BackoffConfig) *Scheduler {
	if cfg.Initial <= 0 {
		cfg.Initial = time.Second
	}
	if cfg.Max <= 0 {
		cfg.Max = cfg.Initial
	}
	return &Scheduler{
		items:     make(map[string]scheduledAttempt),
		cfg:       cfg,
		afterFunc: func(d time.Duration, f func()) timer { return time.AfterFunc(d, f) },
		done:      make(chan struct{}),
	}
}

// Cancels any pending timer and clears attempts for key.
func (s *Scheduler) Cancel(key string) {
	s.itemsMutex.Lock()
	defer s.itemsMutex.Unlock()
	if s.isClosed() {
		return
	}
	if it, ok := s.items[key]; ok {
		it.clearTimer()
		delete(s.items, key)
	}
}

// Stop cancels any pending timer for key but preserves the attempt counter.
// Useful when we want to pause retries without resetting backoff attempts.
func (s *Scheduler) Stop(key string) {
	s.itemsMutex.Lock()
	defer s.itemsMutex.Unlock()
	if s.isClosed() {
		return
	}
	if it, ok := s.items[key]; ok {
		it.clearTimer() // preserve attempts, just drop pending timer
		s.items[key] = it
	}
}

// Schedule schedules an attempt with backoff for key. The attempt function
// receives the current attempt number (1-based). It must return true on success
// (to stop retries) or false to continue retrying.
func (s *Scheduler) Schedule(key string, attempt func(attempts int) bool) {
	// hold mutex first to ensure we don't Schedule concurrently while the Close method runs
	s.itemsMutex.Lock()
	defer s.itemsMutex.Unlock()
	if s.isClosed() {
		return
	}
	// Stop any existing timer to avoid overlapping attempts.
	it := s.items[key]
	it.clearTimer()
	it.attempt++
	attempts := max(it.attempt, 1)
	delay := s.computeDelay(attempts)
	t := s.afterFunc(delay, func() {
		// Bail out quickly if closed; do not reschedule.
		if s.isClosed() {
			return
		}
		// Run attempt outside of lock.
		if success := attempt(attempts); success {
			// Stop the current timer but preserve attempts to avoid backoff reset
			// until the connection proves stable.
			s.itemsMutex.Lock()
			if it2, ok := s.items[key]; ok {
				it2.clearTimer()
				s.items[key] = it2
			}
			s.itemsMutex.Unlock()
			return
		}
		// Reschedule next attempt.
		s.Schedule(key, attempt)
	})
	it.t = t
	s.items[key] = it
}

func (s *Scheduler) computeDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	exp := min(attempts-1, 20) // prevent overflow
	factor := time.Duration(1 << exp)
	d := min(s.cfg.Initial*factor, s.cfg.Max)
	if s.cfg.Jitter > 0 {
		//nolint:gosec // non-crypto jitter is fine
		j := time.Duration(rand.Int63n(int64(s.cfg.Jitter)))
		d += j
	}
	return d
}

// Close stops all pending timers, prevents new scheduling, and signals completion.
// Callers can wait on Done() to ensure closure is visible.
func (s *Scheduler) Close() {
	s.itemsMutex.Lock()
	defer s.itemsMutex.Unlock()
	for k, it := range s.items {
		it.clearTimer()
		delete(s.items, k)
	}
	s.closeOnce.Do(func() { close(s.done) })
}

// Done returns a channel that is closed when the scheduler is closed.
func (s *Scheduler) Done() <-chan struct{} { return s.done }

// isClosed reports whether Close() has been called.
// It performs a non-blocking read on the done channel.
func (s *Scheduler) isClosed() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}
