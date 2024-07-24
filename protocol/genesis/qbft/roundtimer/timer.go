package roundtimer

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/timer.go -source=./timer.go

type OnRoundTimeoutF func(round genesisspecqbft.Round)

const (
	QuickTimeoutThreshold = genesisspecqbft.Round(8)
	QuickTimeout          = 2 * time.Second
	SlowTimeout           = 2 * time.Minute
)

// Timer is an interface for a round timer, calling the UponRoundTimeout when times out
type Timer interface {
	// TimeoutForRound will reset running timer if exists and will start a new timer for a specific round
	TimeoutForRound(height genesisspecqbft.Height, round genesisspecqbft.Round)
}

type BeaconNetwork interface {
	GetSlotStartTime(slot phase0.Slot) time.Time
	SlotDurationSec() time.Duration
}

type TimeoutOptions struct {
	quickThreshold genesisspecqbft.Round
	quick          time.Duration
	slow           time.Duration
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
	// timeoutOptions holds the timeoutOptions for the timer
	timeoutOptions TimeoutOptions
	// role is the role of the instance
	role genesisspectypes.BeaconRole
	// beaconNetwork is the beacon network
	beaconNetwork BeaconNetwork
}

// New creates a new instance of RoundTimer.
func New(pctx context.Context, beaconNetwork BeaconNetwork, role genesisspectypes.BeaconRole, done OnRoundTimeoutF) *RoundTimer {
	ctx, cancelCtx := context.WithCancel(pctx)
	return &RoundTimer{
		mtx:           &sync.RWMutex{},
		ctx:           ctx,
		cancelCtx:     cancelCtx,
		timer:         nil,
		done:          done,
		role:          role,
		beaconNetwork: beaconNetwork,
		timeoutOptions: TimeoutOptions{
			quickThreshold: QuickTimeoutThreshold,
			quick:          QuickTimeout,
			slow:           SlowTimeout,
		},
	}
}

// RoundTimeout calculates the timeout duration for a specific role, height, and round.
//
// Timeout Rules:
// - For roles BNRoleAttester and BNRoleSyncCommittee, the base timeout is 1/3 of the slot duration.
// - For roles BNRoleAggregator and BNRoleSyncCommitteeContribution, the base timeout is 2/3 of the slot duration.
// - For role BNRoleProposer, the timeout is either quickTimeout or slowTimeout, depending on the round.
//
// Additional Timeout:
// - For rounds less than or equal to quickThreshold, the additional timeout is 'quick' seconds.
// - For rounds greater than quickThreshold, the additional timeout is 'slow' seconds.
//
// SIP Reference:
// For more details, see SIP at https://github.com/ssvlabs/SIPs/pull/22
//
// TODO: Update SIP for Deterministic Round Timeout
// TODO: Decide if to make the proposer timeout deterministic
//
// Synchronization Note:
// To ensure synchronized timeouts across instances, the timeout is based on the duty start time,
// which is calculated from the slot height. The base timeout is set based on the role,
// and the additional timeout is added based on the round number.
func (t *RoundTimer) RoundTimeout(height genesisspecqbft.Height, round genesisspecqbft.Round) time.Duration {
	// Initialize duration to zero
	var baseDuration time.Duration

	// Set base duration based on role
	switch t.role {
	case genesisspectypes.BNRoleAttester, genesisspectypes.BNRoleSyncCommittee:
		// third of the slot time
		baseDuration = t.beaconNetwork.SlotDurationSec() / 3
	case genesisspectypes.BNRoleAggregator, genesisspectypes.BNRoleSyncCommitteeContribution:
		// two-third of the slot time
		baseDuration = t.beaconNetwork.SlotDurationSec() / 3 * 2
	default:
		if round <= t.timeoutOptions.quickThreshold {
			return t.timeoutOptions.quick
		}
		return t.timeoutOptions.slow
	}

	// Calculate additional timeout based on round
	var additionalTimeout time.Duration
	if round <= t.timeoutOptions.quickThreshold {
		additionalTimeout = time.Duration(int(round)) * t.timeoutOptions.quick
	} else {
		quickPortion := time.Duration(t.timeoutOptions.quickThreshold) * t.timeoutOptions.quick
		slowPortion := time.Duration(int(round-t.timeoutOptions.quickThreshold)) * t.timeoutOptions.slow
		additionalTimeout = quickPortion + slowPortion
	}

	// Combine base duration and additional timeout
	timeoutDuration := baseDuration + additionalTimeout

	// Get the start time of the duty
	dutyStartTime := t.beaconNetwork.GetSlotStartTime(phase0.Slot(height))

	// Calculate the time until the duty should start plus the timeout duration
	return time.Until(dutyStartTime.Add(timeoutDuration))
}

// OnTimeout sets a function called on timeout.
func (t *RoundTimer) OnTimeout(done OnRoundTimeoutF) {
	t.mtx.Lock() // write to t.done
	defer t.mtx.Unlock()

	t.done = done
}

// Round returns a round.
func (t *RoundTimer) Round() genesisspecqbft.Round {
	return genesisspecqbft.Round(atomic.LoadInt64(&t.round))
}

// TimeoutForRound times out for a given round.
func (t *RoundTimer) TimeoutForRound(height genesisspecqbft.Height, round genesisspecqbft.Round) {
	atomic.StoreInt64(&t.round, int64(round))
	timeout := t.RoundTimeout(height, round)

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

func (t *RoundTimer) waitForRound(round genesisspecqbft.Round, timeout <-chan time.Time) {
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
