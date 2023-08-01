package roundtimer

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
)

type RoundTimeoutFunc func(specqbft.Round) time.Duration
type OnRoundTimeoutF func(round specqbft.Round)

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
	mtx *sync.RWMutex
	ctx context.Context
	// cancelCtx cancels the current context, will be called from Kill()
	cancelCtx context.CancelFunc
	// timer is the underlying time.Timer
	timer *time.Timer
	// result holds the result of the timer
	done OnRoundTimeoutF
	// round is the current round of the timer
	round int64

	roundTimeout RoundTimeoutFunc
}

// New creates a new instance of RoundTimer.
func New(pctx context.Context, done OnRoundTimeoutF) *RoundTimer {
	ctx, cancelCtx := context.WithCancel(pctx)
	return &RoundTimer{
		mtx:          &sync.RWMutex{},
		ctx:          ctx,
		cancelCtx:    cancelCtx,
		timer:        nil,
		done:         done,
		roundTimeout: RoundTimeout,
	}
}

// OnTimeout sets a function called on timeout.
func (t *RoundTimer) OnTimeout(done OnRoundTimeoutF) {
	t.mtx.Lock() // write to t.done
	defer t.mtx.Unlock()

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
	select {
	case <-ctx.Done():
	case <-timeout:
		if t.Round() == round {
			func() {
				t.mtx.RLock() // read t.done
				defer t.mtx.RUnlock()
				if done := t.done; done != nil {
					done(round)
				}
			}()
		}
	}
}
