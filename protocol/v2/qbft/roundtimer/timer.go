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

type OnRoundTimeoutF func(round specqbft.Round)

const (
	QuickTimeoutThreshold = specqbft.Round(8)
	QuickTimeout          = 2 * time.Second
	SlowTimeout           = 2 * time.Minute
)

var CutOffRound specqbft.Round = specqbft.Round(specqbft.CutoffRound)

type TimeoutOptions struct {
	quickThreshold specqbft.Round
	quick          time.Duration
	slow           time.Duration
}

// RoundTimer manages round timeouts for a single duty.
// Created per duty with the callback wired at construction.
// Implements specqbft.Timer.
type RoundTimer struct {
	ctx    context.Context
	cancel context.CancelFunc

	timeoutOptions TimeoutOptions
	role           spectypes.RunnerRole
	beaconConfig   *networkconfig.Beacon

	// callback is a func called when currently stored round times out.
	callback OnRoundTimeoutF

	mtx    *sync.RWMutex
	height specqbft.Height
	round  specqbft.Round
	timer  *time.Timer
}

// New creates a per-duty RoundTimer with the callback wired at construction.
// callback must not be nil.
func New(ctx context.Context, beaconConfig *networkconfig.Beacon, role spectypes.RunnerRole, height specqbft.Height, callback OnRoundTimeoutF) *RoundTimer {
	ctx, cancel := context.WithCancel(ctx)

	return &RoundTimer{
		ctx:          ctx,
		cancel:       cancel,
		beaconConfig: beaconConfig,
		timeoutOptions: TimeoutOptions{
			quickThreshold: QuickTimeoutThreshold,
			quick:          QuickTimeout,
			slow:           SlowTimeout,
		},
		role:     role,
		callback: callback,
		mtx:      &sync.RWMutex{},
		height:   height,
		round:    specqbft.NoRound, // set in TimeoutForRound
		timer:    nil,              // set in TimeoutForRound
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
	dutyStartTime := t.beaconConfig.SlotStartTime(phase0.Slot(t.height))

	// Calculate the time until the duty should start plus the timeout duration
	return time.Until(dutyStartTime.Add(timeoutDuration))
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
