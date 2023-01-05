package roundtimer

import (
	"context"
	"sync/atomic"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"go.uber.org/zap"
)

type RoundTimeoutFunc func(specqbft.Round) time.Duration

var (
	quickTimeoutThreshold = specqbft.Round(8)
	quickTimeout          = 2 * time.Second
	slowTimeout           = 2 * time.Minute
)

// RoundTimeout returns the number of seconds until next timeout for a give round.
// if the round is smaller than 8 -> 2s; otherwise -> 2m
// see SIP https://github.com/bloxapp/SIPs/pull/22
func RoundTimeout(r specqbft.Round) time.Duration {
	if r <= quickTimeoutThreshold {
		return quickTimeout
	}
	return slowTimeout
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

	roundTimeout RoundTimeoutFunc
}

// New creates a new instance of RoundTimer.
func New(pctx context.Context, logger *zap.Logger, done func()) *RoundTimer {
	ctx, cancelCtx := context.WithCancel(pctx)
	return &RoundTimer{
		ctx:          ctx,
		cancelCtx:    cancelCtx,
		logger:       logger,
		timer:        nil,
		done:         done,
		roundTimeout: RoundTimeout,
	}
}

// OnTimeout sets a function called on timeout.
func (t *RoundTimer) OnTimeout(done func()) {
	t.done = done
}

// Round returns a round.
func (t *RoundTimer) Round() specqbft.Round {
	return specqbft.Round(atomic.LoadInt64(&t.round))
}

// TimeoutForRound times out for a given round.
func (t *RoundTimer) TimeoutForRound(round specqbft.Round) {
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

func (t *RoundTimer) waitForRound(round specqbft.Round, timeout <-chan time.Time) {
	ctx, cancel := context.WithCancel(t.ctx)
	defer cancel()
	done := t.done
	select {
	case <-ctx.Done():
	case <-timeout:
		if t.Round() == round {
			if done != nil {
				done()
			}
		}
	}
}
