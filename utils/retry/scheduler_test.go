package retry

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeTimer mimics *time.Timer Stop behavior.
type fakeTimer struct {
	mu      sync.Mutex
	stopped bool
}

func (t *fakeTimer) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped {
		return false
	}
	t.stopped = true
	return true
}

// scheduled event container
type scheduled struct {
	after time.Duration
	fn    func()
	timer *fakeTimer
}

// fakeClock records and controls scheduled callbacks.
type fakeClock struct {
	mu     sync.Mutex
	events []scheduled
}

func (fc *fakeClock) AfterFunc(d time.Duration, f func()) timer {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.events = append(fc.events, scheduled{after: d, fn: f, timer: &fakeTimer{}})
	return &fakeTimer{}
}

func (fc *fakeClock) pop() (scheduled, bool) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if len(fc.events) == 0 {
		return scheduled{}, false
	}
	ev := fc.events[0]
	fc.events = fc.events[1:]
	return ev, true
}

// newWithClock allows tests to inject a custom clock implementation.
func newWithClock(cfg BackoffConfig, clk *fakeClock) *Scheduler {
	s := NewScheduler(cfg)
	if clk != nil {
		s.afterFunc = clk.AfterFunc
	}
	return s
}

func TestScheduler_BackoffSequence(t *testing.T) {
	fc := &fakeClock{}
	cfg := BackoffConfig{Initial: 10 * time.Millisecond, Max: 80 * time.Millisecond}
	s := newWithClock(cfg, fc)

	// attempt always fails for first 4 runs, then succeeds
	count := 0
	s.Schedule("k", func(_ int) bool { count++; return count >= 5 })

	// Execute scheduled functions one by one and record delays
	var delays []time.Duration
	for i := 0; i < 5; i++ {
		ev, ok := fc.pop()
		require.True(t, ok)
		delays = append(delays, ev.after)
		// run callback
		ev.fn()
	}

	require.Equal(t, []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond, 80 * time.Millisecond, 80 * time.Millisecond}, delays)
}

func TestScheduler_Cancel(t *testing.T) {
	fc := &fakeClock{}
	cfg := BackoffConfig{Initial: 5 * time.Millisecond, Max: 20 * time.Millisecond}
	s := newWithClock(cfg, fc)

	// Schedule twice, then cancel and schedule again; verify first delay is used again
	i := 0
	s.Schedule("x", func(_ int) bool { i++; return false })
	ev1, ok := fc.pop()
	require.True(t, ok)
	require.Equal(t, 5*time.Millisecond, ev1.after)
	ev1.fn() // reschedule
	ev2, ok := fc.pop()
	require.True(t, ok)
	require.Equal(t, 10*time.Millisecond, ev2.after)

	s.Cancel("x")

	// Schedule anew
	s.Schedule("x", func(_ int) bool { return false })
	ev3, ok := fc.pop()
	require.True(t, ok)
	require.Equal(t, 5*time.Millisecond, ev3.after)
}

func TestScheduler_ClosePreventsNewSchedules(t *testing.T) {
	fc := &fakeClock{}
	cfg := BackoffConfig{Initial: 5 * time.Millisecond, Max: 20 * time.Millisecond}
	s := newWithClock(cfg, fc)

	// Schedule one event then close.
	s.Schedule("x", func(_ int) bool { return false })
	if _, ok := fc.pop(); !ok {
		t.Fatalf("expected one scheduled event before close")
	}

	s.Close()
	<-s.Done() // wait for close signal

	// Scheduling after close should be a no-op (no new events recorded).
	s.Schedule("y", func(_ int) bool { return false })
	if _, ok := fc.pop(); ok {
		t.Fatalf("did not expect events after close")
	}
}

func TestScheduler_ClosePreventsReschedule(t *testing.T) {
	fc := &fakeClock{}
	cfg := BackoffConfig{Initial: 5 * time.Millisecond, Max: 20 * time.Millisecond}
	s := newWithClock(cfg, fc)

	// Schedule; then close before running the callback.
	s.Schedule("x", func(_ int) bool { return false })
	ev, ok := fc.pop()
	require.True(t, ok)

	// Close the scheduler; callback should observe closed and avoid rescheduling.
	s.Close()
	<-s.Done()

	// Run the pending callback; it should early-return and NOT reschedule.
	ev.fn()
	if _, ok := fc.pop(); ok {
		t.Fatalf("did not expect reschedule after close")
	}
}
