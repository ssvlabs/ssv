package roundtimer

import (
	"sync"
	"time"
)

// RoundTimer is a wrapper around timer to fit the use in an iBFT instance
type RoundTimer struct {
	timer   *time.Timer
	cancelC chan bool
	resC    chan bool

	stopped     bool
	stoppedLock sync.Mutex
}

// New returns a new instance of RoundTimer
func New() *RoundTimer {
	return &RoundTimer{
		timer:       nil,
		cancelC:     make(chan bool),
		resC:        make(chan bool),
		stopped:     true,
		stoppedLock: sync.Mutex{},
	}
}

// ResultChan returns the result chan
// true if the timer lapsed or false if it was stopped
func (t *RoundTimer) ResultChan() chan bool {
	return t.resC
}

// Reset will return a channel that sends true if the timer lapsed or false if it was cancelled
// If Start is called more than once, the first timer and chan are returned and used
func (t *RoundTimer) Reset(d time.Duration) {
	t.stoppedLock.Lock()
	defer t.stoppedLock.Unlock()

	if t.timer != nil {
		t.timer.Stop()
		t.timer.Reset(d)
	} else {
		t.timer = time.NewTimer(d)
	}

	t.stopped = false

	go t.eventLoop()
}

// Stopped returns true if there is no running timer
func (t *RoundTimer) Stopped() bool {
	t.stoppedLock.Lock()
	defer t.stoppedLock.Unlock()
	return t.stopped
}

// Stop will stop the timer and send false on the result chan
func (t *RoundTimer) Stop() {
	if t.timer != nil {
		if t.timer.Stop() {
			t.cancelC <- true
		}
	}
}

func (t *RoundTimer) eventLoop() {
	select {
	case <-t.timer.C:
		t.markStopped()
		t.resC <- true
	case <-t.cancelC:
		t.markStopped()
		t.resC <- false
	}
}

// markStopped set stopped flag to true
func (t *RoundTimer) markStopped() {
	t.stoppedLock.Lock()
	defer t.stoppedLock.Unlock()

	t.stopped = true
}
