package roundtimer

import (
	"context"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/utils/casts"
)

type OnRoundTimeoutF func(round specqbft.Round)

const (
	QuickTimeoutThreshold = specqbft.Round(8)
	QuickTimeout          = 2 * time.Second
	SlowTimeout           = 2 * time.Minute
)

var CutOffRound specqbft.Round = specqbft.Round(specqbft.CutoffRound)

// roundTimeoutForRound returns the time-into-slot at which the given round will time out
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
func roundTimeoutForRound(role spectypes.RunnerRole, slotDuration time.Duration, round specqbft.Round) time.Duration {
	headStart := round1HeadStart(role, slotDuration)
	if round <= QuickTimeoutThreshold {
		return headStart + casts.DurationFromUint64(uint64(round))*QuickTimeout
	}
	quickPortion := casts.DurationFromUint64(uint64(QuickTimeoutThreshold)) * QuickTimeout
	slowPortion := casts.DurationFromUint64(uint64(round-QuickTimeoutThreshold)) * SlowTimeout
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
	case ssvtypes.RoleAggregator, ssvtypes.RoleSyncCommitteeContribution:
		return slotDuration / 3 * 2
	default:
		return 0
	}
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

// RoundTimer manages round timeouts for a single duty.
// Created per duty with the callback wired at construction.
// Implements specqbft.Timer.
type RoundTimer struct {
	ctx    context.Context
	cancel context.CancelFunc

	role         spectypes.RunnerRole
	beaconConfig *networkconfig.Beacon

	// callback is a func called when currently stored round times out.
	callback OnRoundTimeoutF

	mtx   *sync.RWMutex
	slot  phase0.Slot
	round specqbft.Round
	timer *time.Timer
}

// New creates a per-duty RoundTimer with the callback wired at construction.
// callback must not be nil.
func New(ctx context.Context, beaconConfig *networkconfig.Beacon, role spectypes.RunnerRole, slot phase0.Slot, callback OnRoundTimeoutF) *RoundTimer {
	ctx, cancel := context.WithCancel(ctx)

	return &RoundTimer{
		ctx:          ctx,
		cancel:       cancel,
		beaconConfig: beaconConfig,
		role:         role,
		callback:     callback,
		mtx:          &sync.RWMutex{},
		slot:         slot,
		round:        specqbft.NoRound, // set in TimeoutForRound
		timer:        nil,              // set in TimeoutForRound
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
func (t *RoundTimer) RoundTimeout(round specqbft.Round) time.Duration {
	// Proposer runner round timeouts are currently relative to QBFT instance start time, not slot start time:
	// https://github.com/ssvlabs/ssv/issues/2429
	if t.role == spectypes.RoleProposer {
		if round <= QuickTimeoutThreshold {
			return QuickTimeout
		}
		return SlowTimeout
	}

	// Slot-synchronized roles: timeout happens at slot start + roundTimeoutForRound(...).
	dutyStartTime := t.beaconConfig.SlotStartTime(t.slot)
	return time.Until(dutyStartTime.Add(roundTimeoutForRound(t.role, t.beaconConfig.SlotDuration, round)))
}

// TimeoutForRound implements specqbft.Timer.
func (t *RoundTimer) TimeoutForRound(round specqbft.Round) {
	if t.ctx.Err() != nil {
		return
	}

	t.mtx.Lock()
	defer t.mtx.Unlock()

	if t.timer != nil {
		t.timer.Stop()
	}
	t.round = round
	// RoundTimeout can be negative for late-start duties — AfterFunc fires
	// immediately but the callback blocks on RLock until we release mtx.
	t.timer = time.AfterFunc(t.RoundTimeout(round), func() {
		if t.ctx.Err() != nil {
			return
		}
		t.mtx.RLock()
		defer t.mtx.RUnlock()
		// Stale-round guard: if the timer moved to a newer round, this callback is outdated.
		if t.round != round {
			return
		}
		t.callback(round)
	})
}

func (t *RoundTimer) Stop() {
	t.cancel()
	t.mtx.Lock()
	if t.timer != nil {
		t.timer.Stop()
	}
	t.mtx.Unlock()
}
