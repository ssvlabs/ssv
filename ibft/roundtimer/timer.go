package roundtimer

import (
	"sync"
	"time"
)

// RoundTimer is a wrapper around timer to fit the use in an iBFT instance
type RoundTimer struct {
	timer   *time.Timer
	cancelC chan bool
	lapsedC chan bool
	resC    chan bool

	stopped  bool
	syncLock sync.Mutex
}

// New returns a new instance of RoundTimer
func New() *RoundTimer {
	ret := &RoundTimer{
		timer:    nil,
		cancelC:  make(chan bool),
		lapsedC:  make(chan bool),
		stopped:  true,
		syncLock: sync.Mutex{},
	}
	go ret.eventLoop()
	return ret
}

// ResultChan returns the result chan
// true if the timer lapsed or false if it was stopped
func (t *RoundTimer) ResultChan() chan bool {
	t.syncLock.Lock()
	defer t.syncLock.Unlock()

	if t.resC == nil {
		t.resC = make(chan bool)
	}
	return t.resC
}

func (t *RoundTimer) fireChannelEvent(value bool) {
	t.syncLock.Lock()
	defer t.syncLock.Unlock()

	if t.resC != nil {
		t.resC <- value
	}
}

// Reset will return a channel that sends true if the timer lapsed or false if it was cancelled
// If Start is called more than once, the first timer and chan are returned and used
func (t *RoundTimer) Reset(d time.Duration) {
	t.syncLock.Lock()
	defer t.syncLock.Unlock()

	t.stopped = false

	if t.timer != nil {
		t.timer.Stop()
		t.timer.Reset(d)
	} else {
		t.timer = time.AfterFunc(d, func() {
			t.lapsedC <- true
		})
	}
}

// Stopped returns true if there is no running timer
func (t *RoundTimer) Stopped() bool {
	t.syncLock.Lock()
	defer t.syncLock.Unlock()
	return t.stopped
}

// Stop will stop the timer and send false on the result chan
func (t *RoundTimer) Stop() {
	t.syncLock.Lock()
	defer t.syncLock.Unlock()

	if t.timer != nil {
		t.timer.Stop()
		t.cancelC <- true
	}
}

func (t *RoundTimer) eventLoop() {
	for {
		select {
		case <-t.lapsedC:
			t.markStopped()
			t.fireChannelEvent(true)
		case <-t.cancelC:
			t.markStopped()
			t.fireChannelEvent(false)
		}
	}
}

// markStopped set stopped flag to true
func (t *RoundTimer) markStopped() {
	t.syncLock.Lock()
	defer t.syncLock.Unlock()

	t.stopped = true
}
