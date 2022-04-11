package roundtimer

import (
	"context"
	"go.uber.org/zap"
	"sync/atomic"
	"time"
)

// states helps to sync round timer using atomic package
const (
	statePreInit   uint32 = 0
	stateStopped   uint32 = 1
	stateResetting uint32 = 3
	stateRunning   uint32 = 2
)

// RoundTimer helps to manage current instance rounds.
// it should be killed (Kill()) once the instance finished and recreated for each new IBFT instance,
// in that case 'false' is returned in result channel.
// if round has timed-out, the timer returns 'true' in the result channel.
// upon new round, Reset() should be called to reset the timer with the new timeout.
type RoundTimer struct {
	logger *zap.Logger
	ctx    context.Context
	// cancelCtx cancels the current context, will be called from Kill()
	cancelCtx context.CancelFunc
	// timer is the underlying time.Timer
	timer *time.Timer
	// result holds the result of the timer
	result chan bool
	// state helps to sync goroutines on the current state of the timer
	state uint32
}

// New creates a new instance of RoundTimer
func New(pctx context.Context, logger *zap.Logger) *RoundTimer {
	ctx, cancelCtx := context.WithCancel(pctx)
	return &RoundTimer{
		ctx:       ctx,
		cancelCtx: cancelCtx,
		logger:    logger,
		timer:     nil,
		result:    make(chan bool, 1),
		state:     statePreInit,
	}
}

// ResultChan returns the result chan
// true if the timer lapsed or false if it was stopped
func (t *RoundTimer) ResultChan() <-chan bool {
	return t.result
}

// Reset will reset the underlying timer
func (t *RoundTimer) Reset(d time.Duration) {
	if t.ctx.Err() != nil { // timer was killed
		t.logger.Warn("could not reset timer as it was killed already")
		return
	}
	//t.logger.Debug("resetting timer", zap.Duration("timeout", d))
	if atomic.SwapUint32(&t.state, stateResetting) == statePreInit {
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
				t.logger.Debug("round timer was killed")
				t.result <- false
			}
			return
		case <-t.timer.C:
			if atomic.CompareAndSwapUint32(&t.state, stateRunning, stateStopped) {
				t.logger.Debug("round timer was timed-out")
				t.result <- true
			}
		}
	}()
}

// Kill kills the timer
func (t *RoundTimer) Kill() {
	//t.logger.Debug("killing round timer")
	t.cancelCtx()
}

// Stopped returns whether the timer has stopped
func (t *RoundTimer) Stopped() bool {
	state := atomic.LoadUint32(&t.state)
	return state == stateStopped || state == statePreInit
}
