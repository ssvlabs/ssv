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
	syncLock sync.RWMutex
}

// New returns a new instance of RoundTimer
func New() *RoundTimer {
	ret := &RoundTimer{
		timer:    nil,
		cancelC:  make(chan bool),
		lapsedC:  make(chan bool),
		stopped:  true,
		syncLock: sync.RWMutex{},
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

// CloseChan closes the results chan
func (t *RoundTimer) CloseChan() {
	t.syncLock.Lock()
	defer t.syncLock.Unlock()
	if t.resC != nil {
		close(t.resC)
		t.resC = nil
	}
}

func (t *RoundTimer) fireChannelEvent(value bool) {
	t.syncLock.RLock()
	defer t.syncLock.RUnlock()

	if t.resC != nil {
		go func() {
			t.resC <- value
		}()
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
	t.syncLock.RLock()
	defer t.syncLock.RUnlock()
	return t.stopped
}

// Stop will stop the timer and send false on the result chan
func (t *RoundTimer) Stop() {
	t.syncLock.RLock()

	if t.timer != nil {
		t.timer.Stop()
		t.syncLock.RUnlock()
		t.cancelC <- true
	} else {
		t.syncLock.RUnlock()
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
