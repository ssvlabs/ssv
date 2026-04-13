package roundtimer

import (
	"context"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/utils/casts"
)

//go:generate go tool -modfile=../../../../tool.mod mockgen -package=mocks -destination=./mocks/timer.go -source=./timer.go

type OnRoundTimeoutF func(round specqbft.Round)

const (
	QuickTimeoutThreshold = specqbft.Round(8)
	QuickTimeout          = 2 * time.Second
	SlowTimeout           = 2 * time.Minute
)

var CutOffRound specqbft.Round = specqbft.Round(specqbft.CutoffRound)

// Timer is an interface for a round timer, calling the UponRoundTimeout when times out
type Timer interface {
	// TimeoutForRound will reset running timer if exists and will start a new timer for a specific round
	TimeoutForRound(height specqbft.Height, round specqbft.Round)
}

type TimeoutOptions struct {
	quickThreshold specqbft.Round
	quick          time.Duration
	slow           time.Duration
}

// deferredTimeout stores a TimeoutForRound request that arrived before the
// correct callback was registered for this duty's height. OnTimeout replays
// it once the callback is set.
type deferredTimeout struct {
	height specqbft.Height
	round  specqbft.Round
}

// RoundTimer helps to manage current instance rounds.
type RoundTimer struct {
	mtx *sync.RWMutex
	ctx context.Context
	// timer is the underlying time.Timer
	timer *time.Timer
	// callback is the timeout callback for the current duty
	callback OnRoundTimeoutF
	// callbackHeight is the highest duty height seen (registered or deferred)
	callbackHeight specqbft.Height
	// deferred stores a pending arm request when TimeoutForRound is called
	// before the correct callback is registered for this duty
	deferred *deferredTimeout
	// round is the current round of the timer
	round specqbft.Round
	// timeoutOptions holds the timeoutOptions for the timer
	timeoutOptions TimeoutOptions
	// role is the role of the instance
	role spectypes.RunnerRole
	// beaconConfig is the beacon config
	beaconConfig *networkconfig.Beacon
}

// New creates a new instance of RoundTimer. The timeout callback must be
// registered separately via OnTimeout(height, callback) before arming.
func New(ctx context.Context, beaconConfig *networkconfig.Beacon, role spectypes.RunnerRole) *RoundTimer {
	return &RoundTimer{
		mtx:          &sync.RWMutex{},
		ctx:          ctx,
		role:         role,
		beaconConfig: beaconConfig,
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
// For more details, see SIP at https://github.com/bloxapp/SIPs/pull/22
//
// TODO: Update SIP for Deterministic Round Timeout
// TODO: Decide if to make the proposer timeout deterministic
//
// Synchronization Note:
// To ensure synchronized timeouts across instances, the timeout is based on the duty start time,
// which is calculated from the slot height. The base timeout is set based on the role,
// and the additional timeout is added based on the round number.
func (t *RoundTimer) RoundTimeout(height specqbft.Height, round specqbft.Round) time.Duration {
	// Initialize duration to zero
	var baseDuration time.Duration

	// Set base duration based on role
	switch t.role {
	case spectypes.RoleCommittee:
		// third of the slot time
		baseDuration = t.beaconConfig.SlotDuration / 3
	case spectypes.RoleAggregator, spectypes.RoleSyncCommitteeContribution:
		// two-third of the slot time
		baseDuration = t.beaconConfig.SlotDuration / 3 * 2
	default:
		if round <= t.timeoutOptions.quickThreshold {
			return t.timeoutOptions.quick
		}
		return t.timeoutOptions.slow
	}

	// Calculate additional timeout based on round
	var additionalTimeout time.Duration
	if round <= t.timeoutOptions.quickThreshold {
		additionalTimeout = casts.DurationFromUint64(uint64(round)) * t.timeoutOptions.quick
	} else {
		quickPortion := casts.DurationFromUint64(uint64(t.timeoutOptions.quickThreshold)) * t.timeoutOptions.quick
		slowPortion := casts.DurationFromUint64(uint64(round-t.timeoutOptions.quickThreshold)) * t.timeoutOptions.slow
		additionalTimeout = quickPortion + slowPortion
	}

	// Combine base duration and additional timeout
	timeoutDuration := baseDuration + additionalTimeout

	// Get the start time of the duty
	dutyStartTime := t.beaconConfig.SlotStartTime(phase0.Slot(height))

	// Calculate the time until the duty should start plus the timeout duration
	return time.Until(dutyStartTime.Add(timeoutDuration))
}

// armLocked stops any running timer and schedules a new AfterFunc for the
// given height and round. Caller must hold t.mtx.
func (t *RoundTimer) armLocked(height specqbft.Height, round specqbft.Round) {
	if t.timer != nil {
		t.timer.Stop()
	}
	t.round = round
	// RoundTimeout can be negative for late-start duties — AfterFunc fires
	// immediately but the callback blocks on RLock until we release mtx.
	t.timer = time.AfterFunc(t.RoundTimeout(height, round), func() {
		if t.ctx.Err() != nil {
			return
		}
		t.mtx.RLock()
		defer t.mtx.RUnlock()
		if t.callbackHeight != height || t.round != round || t.callback == nil {
			return
		}
		t.callback(round)
	})
}

// OnTimeout registers the timeout callback for the given duty height.
// If TimeoutForRound was called before the callback was registered (deferred),
// OnTimeout replays the arm under the lock.
func (t *RoundTimer) OnTimeout(height specqbft.Height, callback OnRoundTimeoutF) {
	if t.ctx.Err() != nil {
		return
	}

	t.mtx.Lock()
	defer t.mtx.Unlock()

	// Enforce monotonic height increases — reject stale registrations.
	if height < t.callbackHeight {
		return
	}

	t.callback = callback
	t.callbackHeight = height
	d := t.deferred
	t.deferred = nil

	// Replay the deferred arm now that the correct callback is registered.
	// Done under the lock to linearize against concurrent TimeoutForRound
	// calls from message processing (proposal, round-change, timeout bump).
	if d != nil && d.height == height {
		t.armLocked(d.height, d.round)
	}
}

// TimeoutForRound stops any running timer, waits for any in-flight callback to
// finish, and schedules a new callback for the given round. If the callback is
// not yet registered for this duty's height (nil or stale from a previous duty),
// the request is deferred until OnTimeout registers the correct callback.
func (t *RoundTimer) TimeoutForRound(height specqbft.Height, round specqbft.Round) {
	if t.ctx.Err() != nil {
		return
	}

	t.mtx.Lock()
	defer t.mtx.Unlock()

	// Reject stale calls from previous duties.
	if height < t.callbackHeight {
		return
	}

	if t.callback == nil || t.callbackHeight != height {
		// Callback missing or registered for a different duty (stale).
		// Stop any running timer and nil out the stale callback so that an
		// in-flight AfterFunc that is past Stop but waiting on RLock will
		// see callback == nil and bail out.
		if t.timer != nil {
			t.timer.Stop()
			t.timer = nil
		}
		t.callback = nil
		t.callbackHeight = height
		t.deferred = &deferredTimeout{height: height, round: round}
		return
	}
	t.deferred = nil
	t.armLocked(height, round)
}
