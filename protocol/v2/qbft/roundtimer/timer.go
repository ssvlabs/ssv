package roundtimer

import (
	"context"
	"github.com/bloxapp/ssv-spec/qbft"
	"go.uber.org/zap"
	"math"
	"sync/atomic"
	"time"
)

// RoundTimeout is a function that determines the next round timeout
type RoundTimeout func(round qbft.Round) time.Duration

// DefaultRoundTimeout returns the default timeout function (base^round seconds)
func DefaultRoundTimeout(base float64) RoundTimeout {
	return func(round qbft.Round) time.Duration {
		roundTimeout := math.Pow(base, float64(round))
		return time.Duration(float64(time.Second) * roundTimeout)
	}
}

// RoundTimer helps to manage current instance rounds.
type RoundTimer struct {
	logger *zap.Logger
	ctx    context.Context
	// cancelCtx cancels the current context, will be called from Kill()
	cancelCtx context.CancelFunc
	// timer is the underlying time.Timer
	timer *time.Timer
	// result holds the result of the timer
	done func()
	// round is the current round of the timer
	round int64

	roundTimeout RoundTimeout
}

// New creates a new instance of RoundTimer
func New(pctx context.Context, logger *zap.Logger, done func()) *RoundTimer {
	ctx, cancelCtx := context.WithCancel(pctx)
	return &RoundTimer{
		ctx:          ctx,
		cancelCtx:    cancelCtx,
		logger:       logger,
		timer:        nil,
		done:         done,
		roundTimeout: DefaultRoundTimeout(3),
	}
}

func (t *RoundTimer) OnTimeout(done func()) {
	t.done = done
}

func (t *RoundTimer) Round() qbft.Round {
	return qbft.Round(atomic.LoadInt64(&t.round))
}

func (t *RoundTimer) TimeoutForRound(round qbft.Round) {
	atomic.StoreInt64(&t.round, int64(round))
	timeout := t.roundTimeout(round)
	// preparing the underlying timer
	timer := t.timer
	if timer == nil {
		timer = time.NewTimer(timeout)
	} else {
		timer.Stop()
		// draining the channel of existing timer
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(timeout)
	// spawns a new goroutine to listen to the timer
	go t.waitForRound(round, timer.C)
}

func (t *RoundTimer) waitForRound(round qbft.Round, timeout <-chan time.Time) {
	ctx, cancel := context.WithCancel(t.ctx)
	defer cancel()
	done := t.done
	select {
	case <-ctx.Done():
		if t.Round() == round {
			t.logger.Debug("round timer was killed")
		}
	case <-timeout:
		if t.Round() == round {
			t.logger.Debug("round timer was timed-out", zap.Int("round", int(round)))
			if done != nil {
				done()
			}
		}
	}
}
