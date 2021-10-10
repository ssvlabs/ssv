package roundtimer

import (
	"sync"
	"time"
)

// RoundTimer is a wrapper around timer to fit the use in an iBFT instance
type RoundTimer struct {
	timer   *time.Timer
	lapsedC chan bool
	resC    chan bool
	killC   chan bool

	stopped  bool
	syncLock sync.RWMutex
}

// New returns a new instance of RoundTimer
func New() *RoundTimer {
	ret := &RoundTimer{
		timer:    nil,
		lapsedC:  make(chan bool),
		killC:    make(chan bool),
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
	t.syncLock.RLock()
	defer t.syncLock.RUnlock()
	return t.stopped
}

// Kill will stop the timer (without the ability to restart it) and send false on the result chan
func (t *RoundTimer) Kill() {
	t.syncLock.Lock()

	if t.timer != nil {
		t.timer.Stop()
	}
	t.stopped = true
	t.killC <- true

	t.syncLock.Unlock()

	go t.fireChannelEvent(false)
}

func (t *RoundTimer) eventLoop() {
loop:
	for {
		select {
		case <-t.lapsedC:
			t.syncLock.Lock()
			t.stopped = true
			t.syncLock.Unlock()
			go t.fireChannelEvent(true)
		case <-t.killC:
			break loop
		}
	}
}
