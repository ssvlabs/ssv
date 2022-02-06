package roundtimer

import (
	"context"
	"go.uber.org/zap"
	"sync/atomic"
	"time"
)

// states helps to sync round timer using atomic
const (
	stateNotInitialized uint32 = 0
	stateStopped        uint32 = 1
	stateRunning        uint32 = 2
)

// RoundTimer helps to manage current instance rounds.
// it should be killed once the instance finished and recreated for each new instance.
type RoundTimer struct {
	logger *zap.Logger

	ctx       context.Context
	cancelCtx context.CancelFunc

	timer *time.Timer
	// result holds the result of the timer
	// if timed-out the returned value is true
	// otherwise, the timer was killed and the returned value is false
	result chan bool
	// state helps to sync goroutines on the current state of the timer
	state uint32
}

// New creates a new instance of RoundTimer
func New(pctx context.Context, logger *zap.Logger) *RoundTimer {
	ctx, cancelCtx := context.WithCancel(pctx)
	t := &RoundTimer{
		ctx:       ctx,
		cancelCtx: cancelCtx,
		logger:    logger,
		timer:     nil,
		result:    make(chan bool, 1),
		state:     stateNotInitialized,
	}
	return t
}

// ResultChan returns the result chan
// true if the timer lapsed or false if it was stopped
func (t *RoundTimer) ResultChan() <-chan bool {
	return t.result
}

// Reset will reset the underlying timer
func (t *RoundTimer) Reset(d time.Duration) {
	if t.ctx.Err() != nil { // timer was killed
		t.logger.Warn("timer was killed already, reset failed")
		return
	}
	t.logger.Debug("Reset()")
	if atomic.SwapUint32(&t.state, stateRunning) == stateNotInitialized {
		// first reset creates the timer
		t.timer = time.NewTimer(d)
	} else {
		// following calls to reset will reuse the same timer
		t.timer.Stop()
	}
	t.timer.Reset(d)
	atomic.StoreUint32(&t.state, stateRunning)
	go func() {
		select {
		case <-t.ctx.Done():
			if atomic.CompareAndSwapUint32(&t.state, stateRunning, stateStopped) {
				t.logger.Debug("timer killed")
				t.result <- false
			}
			return
		case <-t.timer.C:
			if atomic.CompareAndSwapUint32(&t.state, stateRunning, stateStopped) {
				t.logger.Debug("sending result after time elapsed")
				t.result <- true
			}
		}
	}()
}

// Kill kills the timer
func (t *RoundTimer) Kill() {
	t.logger.Debug("Kill()")
	t.cancelCtx()
}

// Stopped returns whether the timer has stopped
func (t *RoundTimer) Stopped() bool {
	state := atomic.LoadUint32(&t.state)
	return state == stateStopped || state == stateNotInitialized
}
