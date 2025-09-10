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
	Initial time.Duration
	Max     time.Duration
	Jitter  time.Duration
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
	mu        sync.Mutex
	attempts  map[string]int
	timers    map[string]timer
	cfg       BackoffConfig
	afterFunc func(d time.Duration, f func()) timer
	done      chan struct{}
	closeOnce sync.Once
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
		attempts:  make(map[string]int),
		timers:    make(map[string]timer),
		cfg:       cfg,
		afterFunc: func(d time.Duration, f func()) timer { return time.AfterFunc(d, f) },
		done:      make(chan struct{}),
	}
}

// Reset cancels any pending timer and clears attempts for key.
func (s *Scheduler) Cancel(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isClosed() {
		return
	}
	if t, ok := s.timers[key]; ok && t != nil {
		t.Stop()
		delete(s.timers, key)
	}
	delete(s.attempts, key)
}

// Stop cancels any pending timer for key but preserves the attempt counter.
// Useful when we want to pause retries without resetting backoff attempts.
func (s *Scheduler) Stop(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isClosed() {
		return
	}
	if t, ok := s.timers[key]; ok && t != nil {
		t.Stop()
		delete(s.timers, key)
	}
}

// Schedule schedules an attempt with backoff for key. The attempt function
// receives the current attempt number (1-based). It must return true on success
// (to stop retries) or false to continue retrying.
func (s *Scheduler) Schedule(key string, attempt func(attempts int) bool) {
	// hold mutex first to ensure we don't Schedule concurrently while the Close method runs
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isClosed() {
		return
	}
	// Stop any existing timer to avoid overlapping attempts.
	if t, ok := s.timers[key]; ok && t != nil {
		t.Stop()
	}
	s.attempts[key] = s.attempts[key] + 1
	attempts := s.attempts[key]
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
			s.mu.Lock()
			if t, ok := s.timers[key]; ok && t != nil {
				t.Stop()
				delete(s.timers, key)
			}
			s.mu.Unlock()
			return
		}
		// Reschedule next attempt.
		s.Schedule(key, attempt)
	})
	s.timers[key] = t
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
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, t := range s.timers {
		if t != nil {
			t.Stop()
		}
		delete(s.timers, k)
	}
	for k := range s.attempts {
		delete(s.attempts, k)
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
