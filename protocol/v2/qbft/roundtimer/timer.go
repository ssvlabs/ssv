package roundtimer

import (
	"sync/atomic"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/timer.go -source=./timer.go

const (
	QuickTimeoutThreshold = specqbft.Round(8)
	QuickTimeout          = 2 * time.Second
	SlowTimeout           = 2 * time.Minute
)

// Timer is an interface for a round timer, calling the UponRoundTimeout when times out
type Timer interface {
	// TimeoutForRound will reset running timer if exists and will start a new timer for a specific round
	TimeoutForRound(height specqbft.Height, round specqbft.Round)
	GetChannel() <-chan time.Time
	Round() specqbft.Round
	GetRole() spectypes.BeaconRole
	IsActive() bool
}

type BeaconNetwork interface {
	GetSlotStartTime(slot phase0.Slot) time.Time
	SlotDurationSec() time.Duration
}

type TimeoutOptions struct {
	quickThreshold specqbft.Round
	quick          time.Duration
	slow           time.Duration
}

// RoundTimer helps to manage current instance rounds.
type RoundTimer struct {
	// timer is the underlying time.Timer
	timer *time.Timer
	// round is the current round of the timer
	round int64
	// timeoutOptions holds the timeoutOptions for the timer
	timeoutOptions TimeoutOptions
	// role is the role of the instance
	role spectypes.BeaconRole
	// beaconNetwork is the beacon network
	beaconNetwork BeaconNetwork
}

// New creates a new instance of RoundTimer.
func New(beaconNetwork BeaconNetwork, role spectypes.BeaconRole) *RoundTimer {
	return &RoundTimer{
		timer:         nil,
		role:          role,
		beaconNetwork: beaconNetwork,
		timeoutOptions: TimeoutOptions{
			quickThreshold: QuickTimeoutThreshold,
			quick:          QuickTimeout,
			slow:           SlowTimeout,
		},
	}
}

// roundTimeout calculates the timeout duration for a specific role, height, and round.
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
func (t *RoundTimer) roundTimeout(height specqbft.Height, round specqbft.Round) time.Duration {
	// Initialize duration to zero
	var baseDuration time.Duration

	// Set base duration based on role
	switch t.role {
	case spectypes.BNRoleAttester, spectypes.BNRoleSyncCommittee:
		// third of the slot time
		baseDuration = t.beaconNetwork.SlotDurationSec() / 3
	case spectypes.BNRoleAggregator, spectypes.BNRoleSyncCommitteeContribution:
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

// Round returns a round.
func (t *RoundTimer) Round() specqbft.Round {
	return specqbft.Round(atomic.LoadInt64(&t.round))
}

// GetRole returns the role of the RoundTimer.
func (t *RoundTimer) GetRole() spectypes.BeaconRole {
	return t.role
}

// GetChannel returns the channel of the underlying timer which signals
// when the timeout for the current round has occurred.
func (t *RoundTimer) GetChannel() <-chan time.Time {
	return t.timer.C
}

// IsActive checks if the timer is active. The timer is considered active if it has been initialized
// and is currently waiting to fire, and inactive otherwise.
func (t *RoundTimer) IsActive() bool {
	return t.timer != nil
}

// TimeoutForRound times out for a given round.
func (t *RoundTimer) TimeoutForRound(height specqbft.Height, round specqbft.Round) {
	atomic.StoreInt64(&t.round, int64(round))
	timeout := t.roundTimeout(height, round)

	// preparing the underlying timer
	if t.timer == nil {
		t.timer = time.NewTimer(timeout)
		return
	} else {
		t.timer.Stop()
		// draining the channel of existing timer
		select {
		case <-t.timer.C:
		default:
		}
	}
	t.timer.Reset(timeout)
}
