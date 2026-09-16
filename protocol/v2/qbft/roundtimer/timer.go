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
	// QuickTimeout is the per-round budget for every role except the proposer — a fixed
	// network-round-trip allowance, not a slot fraction, so it is not retimed across forks. The
	// slot-synchronized roles absorb the retimed beacon deadlines through their head start instead,
	// which is expressed in IntervalDuration (see round1HeadStart).
	QuickTimeout = 2 * time.Second
	// ProposerQuickTimeout is the proposer's per-round budget (SIP-102). Glamsterdam moves the
	// attestation deadline from 4s to 3s into the slot, and a proposer instance starts ~1.1–1.5s in
	// (RANDAO pre-consensus, ProposerDelay, block retrieval), so with the 2s QuickTimeout a round
	// change would start round 2 past the deadline and turn a recoverable event into a missed block.
	//
	// 1500ms is bounded below by measurement, not by taste: across 30 days of mainnet proposer duties
	// the slowest round 1 that went on to succeed took 1,148ms, so this never fires on a round 1 that
	// would have decided, while 1000ms would have fired on 5–12 real duties a month.
	//
	// It is bounded above by the deadline, and that bound is tight rather than comfortable: round 2
	// starts at instanceStart + 1.5s, so it clears a 3s deadline only for instances starting before
	// 1.5s. SIP-102 sizes this for clusters starting "by ~1.3s" and does not claim to save the slow
	// end of the 1.1–1.5s range — see TestProposerQuickTimeoutBounds, which pins both bounds.
	//
	// Not fork-gated: there is no message, signature or domain change, so this is not a fork, and the
	// pre-Gloas effect is a strict improvement (round 2 starts at ~2.8s instead of ~3.3s, both well
	// inside the 4s deadline). During rollout a mixed cluster is never worse than today — upgraded
	// operators round-change at 1.5s, the rest at 2s, and once f+1 have upgraded the partial-quorum
	// rule pulls the rest along. It is not perfectly seamless either: a pulled-along operator arms its
	// own 2s timer, so upgraded and non-upgraded operators leave round 2 half a second apart. Nothing
	// decided in that gap was going to land inside the deadline anyway.
	DefaultProposerQuickTimeout = 1500 * time.Millisecond
	SlowTimeout                 = 2 * time.Minute
)

// Bounds on the operator-configurable proposer round budget (see WithProposerQuickTimeout and the
// ProposerQuickTimeout config key). The node validates against these at startup.
const (
	// MinProposerQuickTimeout is the slowest round 1 that went on to decide across 30 days of mainnet
	// proposer duties. Below it the timer starts cutting off round 1s that would have succeeded:
	// SIP-102 measured 5-12 such duties a month at 1000ms, which is why it rejected that value.
	//
	// This is a hard floor with no acknowledge-and-proceed override, following ProposerDelayEPBS
	// rather than ProposerDelay. Under the Glamsterdam deadline there is no band here that is merely
	// risky, so there is nothing for an operator to knowingly accept.
	//
	// The floor is the edge of the measured band, not a value that carries margin of its own. An
	// operator who configures exactly this is racing the slowest round 1 we observed: the expiry and
	// the consensus message reach the same queue with no rule that the message wins a tie, so such a
	// round is a coin flip rather than a decide. The margin lives in DefaultProposerQuickTimeout,
	// which clears the observation by 352ms; pick the floor only to deliberately trade that margin
	// away.
	MinProposerQuickTimeout = 1148 * time.Millisecond
	// MaxProposerQuickTimeout is the pre-SIP-102 budget, so an operator can roll back to the previous
	// behavior in-band. Above it a Glamsterdam round change cannot land at all.
	MaxProposerQuickTimeout = 2 * time.Second
)

// Option customizes a RoundTimer at construction.
type Option func(*RoundTimer)

// WithProposerQuickTimeout overrides the proposer's per-round budget for this timer. A non-positive
// duration leaves DefaultProposerQuickTimeout in place, so an unset config value is not an override.
//
// Only the proposer's budget is tunable: the other roles are slot-synchronized, so their round
// boundaries are derived from the beacon deadlines rather than chosen by the operator.
func WithProposerQuickTimeout(d time.Duration) Option {
	return func(t *RoundTimer) {
		if d > 0 {
			t.proposerQuickTimeout = d
		}
	}
}

var CutOffRound specqbft.Round = specqbft.Round(specqbft.CutoffRound)

// defaultQuickTimeoutForRole returns the protocol's per-round budget for rounds at or below
// QuickTimeoutThreshold. The proposer runs a shorter round than everyone else (SIP-102).
//
// This deliberately returns the DEFAULT, not this operator's configured value, because its callers
// reason about other nodes rather than about us: EstimatedRoundAt estimates the round a *peer* is in,
// and a peer runs its own configuration. This operator's own timer takes its budget from
// RoundTimer.quickTimeout instead. (The split is safe for the proposer specifically: message
// validation exempts the proposer from the round-spread check, and production never reaches
// roundTimeoutForRound for it either — so nothing estimates a proposer round from a clock at all.)
func defaultQuickTimeoutForRole(role spectypes.RunnerRole) time.Duration {
	if role == spectypes.RoleProposer {
		return DefaultProposerQuickTimeout
	}
	return QuickTimeout
}

// roundTimeoutForRound returns the time-into-slot at which the given round will time out
// (i.e. transition to round+1) for the given role:
//
//	Round r <= T:  headStart + r * quick
//	Round r >  T:  headStart + T * quick + (r - T) * slow     (T = quickThreshold)
//
// Every role has its own dedicated headStart duration.
func roundTimeoutForRound(role spectypes.RunnerRole, intervalDuration time.Duration, round specqbft.Round) time.Duration {
	headStart := round1HeadStart(role, intervalDuration)
	quick := defaultQuickTimeoutForRole(role)
	if round <= QuickTimeoutThreshold {
		return headStart + casts.DurationFromUint64(uint64(round))*quick
	}
	quickPortion := casts.DurationFromUint64(uint64(QuickTimeoutThreshold)) * quick
	slowPortion := casts.DurationFromUint64(uint64(round-QuickTimeoutThreshold)) * SlowTimeout
	return headStart + quickPortion + slowPortion
}

// round1HeadStart returns the extra time, on top of Round 1's normal quick timeout, that
// Round 1 is allowed to run for a given role. Committee gets one interval as head start
// (time for the block to become available); aggregator, aggregator-committee and
// sync-committee-contribution get two intervals (time for attestations to arrive before
// aggregating); the round-relative roles (see RoundRelativeRole) get zero. The interval is
// IntervalDuration — 1/3 of the slot pre-Gloas, 1/4 from Gloas — so the head starts track the
// retimed attestation/aggregate deadlines across the fork.
//
// Note: this is NOT the time at which Round 1 -> Round 2 transitions — that transition actually
// happens at `slotStart + round1HeadStart + QuickTimeout`, because Round 1 still needs to run its
// own quick timer on top of the head start.
func round1HeadStart(role spectypes.RunnerRole, intervalDuration time.Duration) time.Duration {
	switch role {
	case spectypes.RoleCommittee:
		return intervalDuration
	case ssvtypes.RoleAggregator, ssvtypes.RoleSyncCommitteeContribution, spectypes.RoleAggregatorCommittee:
		return 2 * intervalDuration
	default:
		return 0
	}
}

// EstimatedRoundAt returns the round that should be current for the given runner role (duty-type) at the provided
// elapsed time since slot start (timeIntoSlot).
// Round 1, Round 2, ... Round QuickTimeoutThreshold are considered "quick" (aka short rounds).
// Round QuickTimeoutThreshold+1, Round QuickTimeoutThreshold+2, ... are considered "slow" (aka long rounds).
//
// It answers for a PEER, not for us: it uses the protocol default budget (defaultQuickTimeoutForRole),
// never this operator's configured ProposerQuickTimeout, because a peer runs its own configuration.
// For our own timer use RoundTimer.RoundTimeout. Passing RoleProposer here is not a supported
// production path in any case: message validation exempts the proposer from the round-spread check,
// so nothing estimates a proposer round from a clock.
//
// IMPORTANT: the calculations in this func must be aligned with those in RoundTimeout, those funcs should re-use
// the same code/algo - they currently don't since that would make one of them quite slow, instead the alignment
// is enforced by unit-tests.
func EstimatedRoundAt(role spectypes.RunnerRole, intervalDuration, timeIntoSlot time.Duration) (specqbft.Round, error) {
	// Compute the round directly by inverting the piecewise-linear roundTimeoutOffset formula:
	//   Quick phase (r <= T): offset(r) = headStart + r * quick
	//   Slow phase  (r >  T): offset(r) = headStart + T * quick + (r - T) * slow
	elapsed := timeIntoSlot - round1HeadStart(role, intervalDuration)
	if elapsed < 0 {
		return specqbft.FirstRound, nil
	}

	quick := defaultQuickTimeoutForRole(role)
	quickEnd := casts.DurationFromUint64(uint64(QuickTimeoutThreshold)) * quick
	if elapsed < quickEnd {
		return specqbft.FirstRound + specqbft.Round(elapsed/quick), nil // #nosec G115 -- elapsed is non-negative (guarded above)
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

	// proposerQuickTimeout is the per-round budget used when role is the proposer. Defaults to
	// DefaultProposerQuickTimeout; overridden by WithProposerQuickTimeout.
	proposerQuickTimeout time.Duration

	// callback is a func called when currently stored round times out.
	callback OnRoundTimeoutF

	mtx   *sync.RWMutex
	slot  phase0.Slot
	round specqbft.Round
	timer *time.Timer
}

// New creates a per-duty RoundTimer with the callback wired at construction.
// callback must not be nil.
func New(ctx context.Context, beaconConfig *networkconfig.Beacon, role spectypes.RunnerRole, slot phase0.Slot, callback OnRoundTimeoutF, opts ...Option) *RoundTimer {
	ctx, cancel := context.WithCancel(ctx)

	t := &RoundTimer{
		ctx:                  ctx,
		cancel:               cancel,
		beaconConfig:         beaconConfig,
		role:                 role,
		callback:             callback,
		proposerQuickTimeout: DefaultProposerQuickTimeout,
		mtx:                  &sync.RWMutex{},
		slot:                 slot,
		round:                specqbft.NoRound, // set in TimeoutForRound
		timer:                nil,              // set in TimeoutForRound
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// quickTimeout returns this timer's per-round budget for rounds at or below QuickTimeoutThreshold:
// this operator's configured proposer budget when the role is the proposer, the protocol default
// otherwise.
func (t *RoundTimer) quickTimeout() time.Duration {
	if t.role == spectypes.RoleProposer {
		return t.proposerQuickTimeout
	}
	return QuickTimeout
}

// RoundRelativeRole reports whether the role's QBFT round timeouts are relative to the instance's
// start rather than synchronized to the slot: the proposer (round-relative until
// https://github.com/ssvlabs/ssv/issues/2429). Message validation keys its round-spread exemption off
// this same predicate, so the two stay in step.
func RoundRelativeRole(role spectypes.RunnerRole) bool {
	return role == spectypes.RoleProposer
}

// RoundTimeout returns the duration to wait before timing out the given round.
//
// For the round-relative roles (RoundRelativeRole), the timeout is not slot-synchronized:
//   - rounds <= QuickTimeoutThreshold → this timer's quick timeout (RoundTimer.quickTimeout), which
//     for the proposer is the operator-configurable budget
//   - rounds >  QuickTimeoutThreshold → SlowTimeout
//
// The proposer is the only round-relative role today. Message validation caps its messages at round 2,
// so in practice it never reaches QuickTimeoutThreshold and the SlowTimeout branch is unreachable for
// it; the branch is kept because RoundTimeout is defined for any round, not because a proposer can
// reach one. Making the instance itself stop at that cap is ssvlabs/ssv#3041, not this change.
//
// For all other roles, the timeout is slot-synchronized via roundTimeoutForRound:
// it returns time.Until(slotStart + roundTimeoutForRound(role, IntervalDuration(slot), round)),
// so the result can be negative for duties that started late. The base timeout is one interval
// (attester/sync-committee) or two intervals (aggregator/sync-contribution/aggregator-committee);
// IntervalDuration is 1/3 of the slot before Gloas, 1/4 from Gloas on (SIP #94 §1).
func (t *RoundTimer) RoundTimeout(round specqbft.Round) time.Duration {
	// Proposer round timeouts are relative to QBFT instance start time, not slot start time (see
	// RoundRelativeRole).
	if RoundRelativeRole(t.role) {
		if round <= QuickTimeoutThreshold {
			return t.quickTimeout()
		}
		return SlowTimeout
	}

	// Slot-synchronized roles: timeout happens at slot start + roundTimeoutForRound(...).
	dutyStartTime := t.beaconConfig.SlotStartTime(t.slot)
	return time.Until(dutyStartTime.Add(roundTimeoutForRound(t.role, t.beaconConfig.IntervalDuration(t.slot), round)))
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
