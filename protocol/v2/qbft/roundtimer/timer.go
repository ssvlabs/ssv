package roundtimer

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

type TimeAtSlotFunc func(slot phase0.Slot) int64
type RoundTimeoutFunc func(TimeAtSlotFunc, spectypes.BeaconRole, specqbft.Height, specqbft.Round) time.Duration

var (
	quickTimeoutThreshold = specqbft.Round(8)
	// TODO: Update SIP for Deterministic Round Timeout
	// firstRoundTimeout specifies the duration to wait after the dutyStartTime for ATTESTER/SYNC_COMMITTEE roles.
	// before considering the first round as timed out. This ensures that all instances
	// have a synchronized and predetermined amount of time to perform their first-round duties.
	// the value is 6 sec (1/3 of the slot time [the duty should be executed from] + 2 sec [same as quickTimeout])
	firstRoundTimeout = 6 * time.Second
	quickTimeout      = 2 * time.Second
	slowTimeout       = 2 * time.Minute
)

// Timer is an interface for a round timer, calling the UponRoundTimeout when times out
type Timer interface {
	// TimeoutForRound will reset running timer if exists and will start a new timer for a specific round
	TimeoutForRound(height specqbft.Height, round specqbft.Round)
}

// RoundTimeout returns the number of seconds until next timeout for a give round.
// if the round is smaller than 8 -> 2s; otherwise -> 2m
// see SIP https://github.com/bloxapp/SIPs/pull/22
// TODO: Update SIP for Deterministic Round Timeout
// The new logic accommodates starting instances based on block arrival (either as attester or sync committee).
// This creates a scenario where each instance in a committee could initiate their timers at different times,
// leading to varying timeouts for the first round across instances.
// To synchronize timeouts across all instances, we'll base it on the duty start time,
// which is calculated from the slot height.
func RoundTimeout(timeAtSlotF TimeAtSlotFunc, role spectypes.BeaconRole, height specqbft.Height, round specqbft.Round) time.Duration {
	if round == specqbft.FirstRound && (role == spectypes.BNRoleAttester || role == spectypes.BNRoleSyncCommittee) {
		dutyStartTime := time.Unix(timeAtSlotF(phase0.Slot(height)), 0)
		return time.Until(dutyStartTime.Add(firstRoundTimeout))
	}

	if round <= quickTimeoutThreshold {
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
	done func()
	// round is the current round of the timer
	round int64
	// roundTimeout is a function that returns the timeout for a given round
	roundTimeout RoundTimeoutFunc
	// role is the role of the instance
	role spectypes.BeaconRole
	// timeAtSlotFunc is a function that returns the time at a given slot
	timeAtSlotFunc TimeAtSlotFunc
}

// New creates a new instance of RoundTimer.
func New(pctx context.Context, role spectypes.BeaconRole, timeAtSlotFunc TimeAtSlotFunc, done func()) *RoundTimer {
	ctx, cancelCtx := context.WithCancel(pctx)
	return &RoundTimer{
		mtx:            &sync.RWMutex{},
		ctx:            ctx,
		cancelCtx:      cancelCtx,
		timer:          nil,
		done:           done,
		roundTimeout:   RoundTimeout,
		role:           role,
		timeAtSlotFunc: timeAtSlotFunc,
	}
}

// OnTimeout sets a function called on timeout.
func (t *RoundTimer) OnTimeout(done func()) {
	t.mtx.Lock() // write to t.done
	defer t.mtx.Unlock()

	t.done = done
}

// Round returns a round.
func (t *RoundTimer) Round() specqbft.Round {
	return specqbft.Round(atomic.LoadInt64(&t.round))
}

// TimeoutForRound times out for a given round.
func (t *RoundTimer) TimeoutForRound(height specqbft.Height, round specqbft.Round) {
	atomic.StoreInt64(&t.round, int64(round))

	timeout := t.roundTimeout(t.timeAtSlotFunc, t.role, height, round)

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
					done()
				}
			}()
		}
	}
}
