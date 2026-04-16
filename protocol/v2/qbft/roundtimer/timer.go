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

// roundTimeoutOffset returns the time-into-slot at which the given round will time out
// (i.e. transition to round+1) for the given role:
//
//	Round 1     ends at  headStart + 1 * quick
//	Round 2     ends at  headStart + 2 * quick
//	...
//	Round T     ends at  headStart + T * quick               (T = quickThreshold)
//	Round T+1   ends at  headStart + T * quick + 1 * slow
//	Round T+2   ends at  headStart + T * quick + 2 * slow
//
// Every role has its own dedicated headStart duration.
func (o TimeoutOptions) roundTimeoutOffset(role spectypes.RunnerRole, slotDuration time.Duration, round specqbft.Round) time.Duration {
	headStart := round1HeadStart(role, slotDuration)
	if round <= o.quickThreshold {
		return headStart + casts.DurationFromUint64(uint64(round))*o.quick
	}
	quickPortion := casts.DurationFromUint64(uint64(o.quickThreshold)) * o.quick
	slowPortion := casts.DurationFromUint64(uint64(round-o.quickThreshold)) * o.slow
	return headStart + quickPortion + slowPortion
}

// round1HeadStart returns the extra time, on top of Round 1's normal quick timeout, that
// Round 1 is allowed to run for a given role. Committee gets 1/3 of the slot as head start
// (time for the block to become available); aggregator and sync-committee-contribution get
// 2/3 of the slot (time for attestations to arrive); proposer gets zero.
//
// Note: this is NOT the time at which Round 1 -> Round 2 transitions — that transition actually
// happens at `slotStart + round1HeadStart + QuickTimeout`, because Round 1 still needs to run its
// own quick timer on top of the head start.
func round1HeadStart(role spectypes.RunnerRole, slotDuration time.Duration) time.Duration {
	switch role {
	case spectypes.RoleCommittee:
		return slotDuration / 3
	case spectypes.RoleAggregator, spectypes.RoleSyncCommitteeContribution:
		return slotDuration / 3 * 2
	default:
		return 0
	}
}

// defaultTimeoutOptions are the production timeout parameters. They're used by New() to
// populate each RoundTimer and by EstimatedRoundAt to estimate the current round in the
// same way RoundTimeout drives the timer. Tests that need different quick/slow values
// override RoundTimer.timeoutOptions directly — EstimatedRoundAt is not involved there.
var defaultTimeoutOptions = TimeoutOptions{
	quickThreshold: QuickTimeoutThreshold,
	quick:          QuickTimeout,
	slow:           SlowTimeout,
}

// EstimatedRoundAt returns the round that should be current for the given runner role (duty-type) at the provided
// elapsed time since slot start (timeIntoSlot).
// Round 1, Round 2, ... Round QuickTimeoutThreshold are considered "quick" (aka short rounds).
// Round QuickTimeoutThreshold+1, Round QuickTimeoutThreshold+2, ... are considered "slow" (aka long rounds).
//
// IMPORTANT: the calculations in this func must be aligned with those in RoundTimeout, those funcs should re-use
// the same code/algo - they currently don't since that would make one of them quite slow, instead the alignment
// is enforced by unit-tests.
func EstimatedRoundAt(role spectypes.RunnerRole, slotDuration, timeIntoSlot time.Duration) (specqbft.Round, error) {
	// Compute the round directly by inverting the piecewise-linear roundTimeoutOffset formula:
	//   Quick phase (r <= T): offset(r) = headStart + r * quick
	//   Slow phase  (r >  T): offset(r) = headStart + T * quick + (r - T) * slow
	elapsed := timeIntoSlot - round1HeadStart(role, slotDuration)
	if elapsed < 0 {
		return specqbft.FirstRound, nil
	}

	quickEnd := casts.DurationFromUint64(uint64(QuickTimeoutThreshold)) * QuickTimeout
	if elapsed < quickEnd {
		return specqbft.FirstRound + specqbft.Round(elapsed/QuickTimeout), nil // #nosec G115 -- elapsed is non-negative (guarded above)
	}

	slowElapsed := elapsed - quickEnd
	return specqbft.FirstRound + QuickTimeoutThreshold + specqbft.Round(slowElapsed/SlowTimeout), nil // #nosec G115 -- slowElapsed is non-negative (elapsed >= quickEnd)
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
		mtx:            &sync.RWMutex{},
		ctx:            ctx,
		role:           role,
		beaconConfig:   beaconConfig,
		timeoutOptions: defaultTimeoutOptions,
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
	// Proposer runner round timeouts are currently relative to QBFT instance start time, not slot start time:
	// https://github.com/ssvlabs/ssv/issues/2429
	if t.role == spectypes.RoleProposer {
		if round <= t.timeoutOptions.quickThreshold {
			return t.timeoutOptions.quick
		}
		return t.timeoutOptions.slow
	}

	// Slot-synchronized roles: timeout happens at slot start + roundTimeoutOffset(...).
	dutyStartTime := t.beaconConfig.SlotStartTime(phase0.Slot(height))
	return time.Until(dutyStartTime.Add(t.timeoutOptions.roundTimeoutOffset(t.role, t.beaconConfig.SlotDuration, round)))
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
