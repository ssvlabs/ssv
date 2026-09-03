package roundtimer

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

const (
	// safeTestDelay is a safety buffer (a guesstimate) to use in tests that set expectations making certain
	// assumptions regarding the go-routine scheduling.
	safeTestDelay = 100 * time.Millisecond
)

func TestTimeoutForRound(t *testing.T) {
	quickRound := specqbft.FirstRound
	slowRound := QuickTimeoutThreshold + 1

	roles := []spectypes.RunnerRole{
		spectypes.RoleCommittee,
		ssvtypes.RoleAggregator,
		spectypes.RoleProposer,
		spectypes.RoleEnvelopeProposer,
		ssvtypes.RoleSyncCommitteeContribution,
		spectypes.RoleAggregatorCommittee,
	}

	for _, role := range roles {
		t.Run(fmt.Sprintf("TimeoutForRound - %s: <= quickTimeoutThreshold", role), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testTimeoutForRound(t, role, quickRound)
			})
		})

		t.Run(fmt.Sprintf("TimeoutForRound - %s: > quickTimeoutThreshold", role), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testTimeoutForRound(t, role, slowRound)
			})
		})

		t.Run(fmt.Sprintf("TimeoutForRound - %s: before elapsed", role), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testTimeoutForRoundElapsed(t, role, slowRound)
			})
		})

		t.Run(fmt.Sprintf("TimeoutForRound - %s: timer stored and reused", role), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testTimeoutForRoundTimerStored(t, role, quickRound)
			})
		})

		t.Run(fmt.Sprintf("TimeoutForRound - %s: context canceled before arm", role), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testTimeoutForRoundContextCancelled(t, role, quickRound)
			})
		})

		t.Run(fmt.Sprintf("TimeoutForRound - %s: context canceled after arm", role), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testTimeoutForRoundContextCancelledAfterArm(t, role, quickRound)
			})
		})

		// TODO: Decide if to make the proposer timeout deterministic
		// The round-relative roles (proposer, envelope proposer) are not tested for multiple synchronized
		// timers since their timeouts aren't slot-synchronized.
		if RoundRelativeRole(role) {
			continue
		}

		t.Run(fmt.Sprintf("TimeoutForRound - %s: multiple synchronized timers", role), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testTimeoutForRoundMulti(t, role)
			})
		})
	}
}

func TestEstimatedRoundAt(t *testing.T) {
	const slotDuration = 600 * time.Millisecond

	testBeaconConfig := setupTestBeaconConfig()

	tt := []struct {
		name         string
		role         spectypes.RunnerRole
		timeIntoSlot time.Duration
		want         specqbft.Round
	}{
		{
			name:         "proposer starts quick round timing at slot start",
			role:         spectypes.RoleProposer,
			timeIntoSlot: QuickTimeout,
			want:         specqbft.FirstRound + 1,
		},
		{
			name:         "envelope proposer starts quick round timing at slot start (no head start)",
			role:         spectypes.RoleEnvelopeProposer,
			timeIntoSlot: QuickTimeout,
			want:         specqbft.FirstRound + 1,
		},
		{
			name:         "committee keeps first round until one-third slot delay passes",
			role:         spectypes.RoleCommittee,
			timeIntoSlot: slotDuration / 3,
			want:         specqbft.FirstRound,
		},
		{
			name:         "committee advances after one-third slot delay plus quick timeout",
			role:         spectypes.RoleCommittee,
			timeIntoSlot: slotDuration/3 + QuickTimeout,
			want:         specqbft.FirstRound + 1,
		},
		{
			name:         "aggregator keeps first round until two-third slot delay passes",
			role:         ssvtypes.RoleAggregator,
			timeIntoSlot: slotDuration / 3 * 2,
			want:         specqbft.FirstRound,
		},
		{
			// Basic behavioral parity with the aggregator case above. NOTE: at this 600ms slot the
			// head-start difference (0 vs 400ms) is below one QuickTimeout (2s), so this case alone
			// does NOT discriminate patched vs unpatched — see the realistic-slot regression assertion
			// after the loop (and TestRoundTimeoutOffset) for the actual guard.
			name:         "aggregator-committee keeps first round until two-third slot delay passes",
			role:         spectypes.RoleAggregatorCommittee,
			timeIntoSlot: slotDuration / 3 * 2,
			want:         specqbft.FirstRound,
		},
		{
			name:         "sync committee contribution advances after two-third slot delay plus quick timeout",
			role:         ssvtypes.RoleSyncCommitteeContribution,
			timeIntoSlot: slotDuration/3*2 + QuickTimeout,
			want:         specqbft.FirstRound + 1,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EstimatedRoundAt(tc.role, testBeaconConfig.IntervalDuration(0), tc.timeIntoSlot)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}

	// Discriminating regression guard for the missing RoleAggregatorCommittee head start.
	// At a realistic 12s slot, aggregation data arrives ~2/3 in (8s). With the fix (2/3-slot head
	// start) the round is still 1 there; without it (head start 0) EstimatedRoundAt resolves to
	// round 5 (1 + 8s/QuickTimeout), starting consensus mid-round. Unlike the 600ms table cases
	// above, this assertion fails against the unpatched code.
	t.Run("aggregator-committee resolves round 1 at two-thirds of a realistic 12s slot", func(t *testing.T) {
		const realisticSlot = 12 * time.Second
		round, err := EstimatedRoundAt(spectypes.RoleAggregatorCommittee, realisticSlot, realisticSlot/3*2)
		require.NoError(t, err)
		require.Equal(t, specqbft.FirstRound, round)
	})
}

// TestRoundTimeoutOffset covers the pure helper directly, which is the single source of truth
// both RoundTimeout and EstimatedRoundAt derive from. It uses the production timeout constants
// and picks a slot duration that lands the role-derived head starts on round numbers:
//
//	proposer   → head start = 0
//	committee  → head start = 12s / 3        = 4s
//	aggregator → head start = 12s / 3 * 2    = 8s
func TestRoundTimeoutOffset(t *testing.T) {
	// Use a realistic slot duration (12s) so the numbers line up with the real QuickTimeout (2s) and
	// SlowTimeout (2m) values.
	slotDuration := networkconfig.TestNetwork.SlotDuration
	quickPhase := time.Duration(QuickTimeoutThreshold) * QuickTimeout

	tt := []struct {
		name  string
		role  spectypes.RunnerRole
		round specqbft.Round
		want  time.Duration
	}{
		// Proposer (head start = 0): offset is r*quick for quick rounds.
		{name: "proposer, round 1 (first quick)", role: spectypes.RoleProposer, round: 1, want: QuickTimeout},
		{name: "proposer, round 2", role: spectypes.RoleProposer, round: 2, want: 2 * QuickTimeout},
		{name: "proposer, round 8 (= quickThreshold)", role: spectypes.RoleProposer, round: QuickTimeoutThreshold, want: quickPhase},
		// First slow round: quickThreshold * quick + 1 * slow.
		{name: "proposer, round 9 (first slow)", role: spectypes.RoleProposer, round: QuickTimeoutThreshold + 1, want: quickPhase + SlowTimeout},
		{name: "proposer, round 10", role: spectypes.RoleProposer, round: QuickTimeoutThreshold + 2, want: quickPhase + 2*SlowTimeout},

		// Envelope proposer (head start = 0): the same offsets as the proposer.
		{name: "envelope_proposer, round 1", role: spectypes.RoleEnvelopeProposer, round: 1, want: QuickTimeout},
		{name: "envelope_proposer, round 9 (first slow)", role: spectypes.RoleEnvelopeProposer, round: QuickTimeoutThreshold + 1, want: quickPhase + SlowTimeout},

		// Committee (head start = 4s): offset starts with head start added.
		{name: "committee, round 1", role: spectypes.RoleCommittee, round: 1, want: 4*time.Second + QuickTimeout},
		{name: "committee, round 2", role: spectypes.RoleCommittee, round: 2, want: 4*time.Second + 2*QuickTimeout},
		{name: "committee, round 8", role: spectypes.RoleCommittee, round: QuickTimeoutThreshold, want: 4*time.Second + quickPhase},
		{name: "committee, round 9 (first slow)", role: spectypes.RoleCommittee, round: QuickTimeoutThreshold + 1, want: 4*time.Second + quickPhase + SlowTimeout},

		// Aggregator (head start = 8s): covers the 2/3-slot branch.
		{name: "aggregator, round 1", role: ssvtypes.RoleAggregator, round: 1, want: 8*time.Second + QuickTimeout},
		{name: "aggregator, round 8", role: ssvtypes.RoleAggregator, round: QuickTimeoutThreshold, want: 8*time.Second + quickPhase},
		{name: "aggregator, round 9 (first slow)", role: ssvtypes.RoleAggregator, round: QuickTimeoutThreshold + 1, want: 8*time.Second + quickPhase + SlowTimeout},

		// Sync committee contribution uses the same 2/3-slot branch as aggregator.
		{name: "sync_committee_contribution, round 1", role: ssvtypes.RoleSyncCommitteeContribution, round: 1, want: 8*time.Second + QuickTimeout},

		// Aggregator-committee uses the same 2/3-slot branch as aggregator.
		{name: "aggregator_committee, round 1", role: spectypes.RoleAggregatorCommittee, round: 1, want: 8*time.Second + QuickTimeout},
		{name: "aggregator_committee, round 8", role: spectypes.RoleAggregatorCommittee, round: QuickTimeoutThreshold, want: 8*time.Second + quickPhase},
	}
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			got := roundTimeoutForRound(tc.role, slotDuration/3, tc.round)
			require.Equal(t, tc.want, got)
		})
	}
}

// TestRoundTimeoutOffsetGloasInterval verifies the head starts track IntervalDuration: passing the
// Gloas interval (1/4 of the slot, vs 1/3 pre-Gloas) shrinks the committee/aggregator head starts
// accordingly, so a stalled round 1 falls back to round 2 in step with the retimed deadlines.
func TestRoundTimeoutOffsetGloasInterval(t *testing.T) {
	slotDuration := networkconfig.TestNetwork.SlotDuration
	gloasInterval := slotDuration / 4

	tt := []struct {
		name string
		role spectypes.RunnerRole
		want time.Duration
	}{
		{name: "committee head start = 1 interval", role: spectypes.RoleCommittee, want: slotDuration/4 + QuickTimeout},
		{name: "aggregator head start = 2 intervals", role: ssvtypes.RoleAggregator, want: slotDuration/2 + QuickTimeout},
		{name: "sync_committee_contribution head start = 2 intervals", role: ssvtypes.RoleSyncCommitteeContribution, want: slotDuration/2 + QuickTimeout},
		{name: "proposer head start = 0", role: spectypes.RoleProposer, want: QuickTimeout},
		{name: "envelope proposer head start = 0", role: spectypes.RoleEnvelopeProposer, want: QuickTimeout},
	}
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, roundTimeoutForRound(tc.role, gloasInterval, specqbft.FirstRound))
		})
	}
}

// TestEstimatedRoundAtBoundaries exercises each round's transition boundary for every role.
// For every round r, it checks EstimatedRoundAt at:
//   - offset - 1ns → still in round r
//   - offset exactly → just advanced to round r+1
//   - offset + 1ns → still in round r+1
//
// This catches an off-by-one (`<` vs `<=`) at a round boundary or a wrong starting round —
// neither of which the pre-existing tests directly exercised.
func TestEstimatedRoundAtBoundaries(t *testing.T) {
	// Use a realistic slot duration (12s) so the numbers line up with the real QuickTimeout (2s) and
	// SlowTimeout (2m) values.
	slotDuration := networkconfig.TestNetwork.SlotDuration

	roles := []struct {
		name string
		role spectypes.RunnerRole
	}{
		{"proposer", spectypes.RoleProposer},
		{"envelope_proposer", spectypes.RoleEnvelopeProposer},
		{"committee", spectypes.RoleCommittee},
		{"aggregator", ssvtypes.RoleAggregator},
		{"sync_committee_contribution", ssvtypes.RoleSyncCommitteeContribution},
		{"aggregator_committee", spectypes.RoleAggregatorCommittee},
	}

	for _, rc := range roles {
		t.Run(rc.name, func(t *testing.T) {
			// Walk rounds 1..CutOffRound+2; beyond CutOffRound we've already crossed into
			// "late message" territory but EstimatedRoundAt is still defined and should
			// keep incrementing with the same rules.
			for round := specqbft.Round(1); round <= CutOffRound+2; round++ {
				offset := roundTimeoutForRound(rc.role, slotDuration/3, round)

				// 1 ns before the boundary: round r has not yet timed out.
				got, err := EstimatedRoundAt(rc.role, slotDuration/3, offset-time.Nanosecond)
				require.NoError(t, err)
				require.Equal(t, round, got, "round %d: 1ns before boundary", round)

				// Exactly at the boundary: round r has timed out, we are now in round r+1.
				got, err = EstimatedRoundAt(rc.role, slotDuration/3, offset)
				require.NoError(t, err)
				require.Equal(t, round+1, got, "round %d: exactly at boundary", round)

				// 1 ns after the boundary: still in round r+1 (until next boundary).
				got, err = EstimatedRoundAt(rc.role, slotDuration/3, offset+time.Nanosecond)
				require.NoError(t, err)
				require.Equal(t, round+1, got, "round %d: 1ns after boundary", round)
			}
		})
	}
}

// TestEstimatedRoundAtEdgeCases covers inputs at and before slot start. EstimatedRoundAt
// special-cases them with `if elapsed < 0 { return FirstRound, nil }`; this test
// regression-guards that behavior.
func TestEstimatedRoundAtEdgeCases(t *testing.T) {
	// Use a realistic slot duration (12s) so the numbers line up with the real QuickTimeout (2s) and
	// SlowTimeout (2m) values.
	slotDuration := networkconfig.TestNetwork.SlotDuration

	tt := []struct {
		name         string
		role         spectypes.RunnerRole
		timeIntoSlot time.Duration
	}{
		// timeIntoSlot = 0: all roles should report FirstRound.
		{name: "proposer at slot start", role: spectypes.RoleProposer, timeIntoSlot: 0},
		{name: "committee at slot start", role: spectypes.RoleCommittee, timeIntoSlot: 0},
		{name: "aggregator at slot start", role: ssvtypes.RoleAggregator, timeIntoSlot: 0},
		{name: "sync_contribution at slot start", role: ssvtypes.RoleSyncCommitteeContribution, timeIntoSlot: 0},

		// Negative timeIntoSlot (unreachable in practice — validateSlotTime catches early
		// messages — but the pure function must still be well-defined).
		{name: "proposer 1s early", role: spectypes.RoleProposer, timeIntoSlot: -time.Second},
		{name: "committee 1s early", role: spectypes.RoleCommittee, timeIntoSlot: -time.Second},
		{name: "committee 100s early", role: spectypes.RoleCommittee, timeIntoSlot: -100 * time.Second},

		// Inside the committee head start (2s into a 4s head start) — still Round 1.
		{name: "committee mid-head-start", role: spectypes.RoleCommittee, timeIntoSlot: 2 * time.Second},
		{name: "committee end of head start", role: spectypes.RoleCommittee, timeIntoSlot: slotDuration / 3},
		// Aggregator head start is 8s; at 7s we're still in Round 1.
		{name: "aggregator mid-head-start", role: ssvtypes.RoleAggregator, timeIntoSlot: 7 * time.Second},
	}
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EstimatedRoundAt(tc.role, slotDuration/3, tc.timeIntoSlot)
			require.NoError(t, err)
			require.Equal(t, specqbft.FirstRound, got)
		})
	}
}

// TestRoundTimeoutMatchesRoundTimeoutOffset is a regression guard for RoundTimeout vs the
// shared roundTimeoutForRound helper. Non-proposer RoundTimeout is defined as
//
//	time.Until(slotStart + roundTimeoutForRound(role, slotDuration, round))
//
// so with GenesisTime pinned to `time.Now()` under synctest (frozen clock), slot 0 starts
// "now" and the returned duration must exactly equal roundTimeoutForRound. If anyone changes
// RoundTimeout's math without updating roundTimeoutForRound (or vice versa), this test fails.
func TestRoundTimeoutMatchesRoundTimeoutOffset(t *testing.T) {
	// The round-relative roles (proposer, envelope proposer) don't time from slot start, so they're
	// exempt from the "equals roundTimeoutOffset" property. We cover the slot-synchronized roles only.
	roles := []struct {
		name string
		role spectypes.RunnerRole
	}{
		{"committee", spectypes.RoleCommittee},
		{"aggregator", ssvtypes.RoleAggregator},
		{"sync_committee_contribution", ssvtypes.RoleSyncCommitteeContribution},
	}

	// Nest synctest inside t.Run (not the other way around) — synctest.Test disallows
	// t.Run calls inside its bubble.
	for _, rc := range roles {
		t.Run(rc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				beaconConfig := setupTestBeaconConfig()
				timer := New(t.Context(), beaconConfig, rc.role, 0, func(round specqbft.Round) {})

				for round := specqbft.Round(1); round <= CutOffRound; round++ {
					expected := roundTimeoutForRound(rc.role, beaconConfig.IntervalDuration(0), round)
					got := timer.RoundTimeout(round)
					require.Equal(t, expected, got, "round %d", round)
				}
			})
		})
	}
}

// TestEstimatedRoundAtMatchesRoundTimeout directly cross-validates EstimatedRoundAt against
// RoundTimeout for all roles (including the round-relative ones). If someone changes the formula
// in either function without updating the other, this test fails.
//
// For the slot-synchronized roles, RoundTimeout at frozen time == slot start returns the
// cumulative offset directly. For the round-relative roles, RoundTimeout returns individual
// per-round durations, so we accumulate them to get the boundary at which EstimatedRoundAt
// should advance.
func TestEstimatedRoundAtMatchesRoundTimeout(t *testing.T) {
	roles := []struct {
		name string
		role spectypes.RunnerRole
	}{
		{"proposer", spectypes.RoleProposer},
		{"envelope_proposer", spectypes.RoleEnvelopeProposer},
		{"committee", spectypes.RoleCommittee},
		{"aggregator", ssvtypes.RoleAggregator},
		{"sync_committee_contribution", ssvtypes.RoleSyncCommitteeContribution},
		{"aggregator_committee", spectypes.RoleAggregatorCommittee},
	}

	for _, rc := range roles {
		t.Run(rc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				beaconConfig := setupTestBeaconConfig()
				timer := New(t.Context(), beaconConfig, rc.role, 0, func(round specqbft.Round) {})

				// For the round-relative roles, RoundTimeout returns per-round durations; accumulate them.
				// For other roles, RoundTimeout (at frozen time = slot start) returns
				// the cumulative offset directly.
				var cumulative time.Duration
				for round := specqbft.FirstRound; round <= CutOffRound; round++ {
					rt := timer.RoundTimeout(round)
					if RoundRelativeRole(rc.role) {
						cumulative += rt
					} else {
						cumulative = rt
					}

					// 1 ns before the boundary: still in current round.
					got, err := EstimatedRoundAt(rc.role, beaconConfig.IntervalDuration(0), cumulative-time.Nanosecond)
					require.NoError(t, err)
					require.Equal(t, round, got, "round %d: 1ns before boundary", round)

					// Exactly at the boundary: advanced to next round.
					got, err = EstimatedRoundAt(rc.role, beaconConfig.IntervalDuration(0), cumulative)
					require.NoError(t, err)
					require.Equal(t, round+1, got, "round %d: at boundary", round)
				}
			})
		})
	}
}

func setupTestBeaconConfig() *networkconfig.Beacon {
	config := *networkconfig.TestNetwork.Beacon
	config.SlotDuration = 600 * time.Millisecond
	config.GenesisTime = time.Now()

	return &config
}

func testTimeoutForRound(t *testing.T, role spectypes.RunnerRole, round specqbft.Round) {
	testBeaconConfig := setupTestBeaconConfig()

	count := int32(0)
	onTimeout := func(round specqbft.Round) {
		atomic.AddInt32(&count, 1)
	}

	timer := New(t.Context(), testBeaconConfig, role, 0, onTimeout)

	require.Equal(t, int32(0), atomic.LoadInt32(&count))

	timer.TimeoutForRound(round)
	<-time.After(timer.RoundTimeout(round) + safeTestDelay)
	require.Equal(t, int32(1), atomic.LoadInt32(&count))
}

func testTimeoutForRoundElapsed(t *testing.T, role spectypes.RunnerRole, round specqbft.Round) {
	testBeaconConfig := setupTestBeaconConfig()

	count := int32(0)
	onTimeout := func(round specqbft.Round) {
		atomic.AddInt32(&count, 1)
	}

	timer := New(t.Context(), testBeaconConfig, role, 0, onTimeout)

	timer.TimeoutForRound(specqbft.FirstRound)
	<-time.After(timer.RoundTimeout(specqbft.FirstRound) / 2)
	timer.TimeoutForRound(round) // reset before elapsed
	require.Equal(t, int32(0), atomic.LoadInt32(&count))
	<-time.After(timer.RoundTimeout(round) + safeTestDelay)
	require.Equal(t, int32(1), atomic.LoadInt32(&count))
}

func testTimeoutForRoundTimerStored(t *testing.T, role spectypes.RunnerRole, round specqbft.Round) {
	testBeaconConfig := setupTestBeaconConfig()
	timer := New(t.Context(), testBeaconConfig, role, 0, func(specqbft.Round) {})

	require.Nil(t, timer.timer, "timer should be nil before first call")

	timer.TimeoutForRound(round)

	timer.mtx.RLock()
	firstTimer := timer.timer
	timer.mtx.RUnlock()
	require.NotNil(t, firstTimer, "timer must be stored after TimeoutForRound")

	// Second call must replace the timer (stop old, create new).
	timer.TimeoutForRound(round + 1)

	timer.mtx.RLock()
	secondTimer := timer.timer
	timer.mtx.RUnlock()
	require.NotNil(t, secondTimer, "timer must be stored after second TimeoutForRound")
	require.NotSame(t, firstTimer, secondTimer, "each call must create a new timer")
}

func testTimeoutForRoundContextCancelled(t *testing.T, role spectypes.RunnerRole, round specqbft.Round) {
	testBeaconConfig := setupTestBeaconConfig()

	var count int32
	onTimeout := func(round specqbft.Round) {
		atomic.AddInt32(&count, 1)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel before arming

	timer := New(ctx, testBeaconConfig, role, 0, onTimeout)
	timer.TimeoutForRound(round)

	// Early return should skip timer creation entirely.
	timer.mtx.RLock()
	require.Nil(t, timer.timer, "timer must not be created when context is already canceled")
	timer.mtx.RUnlock()

	// Wait for the full round timeout to confirm no callback fires.
	<-time.After(timer.RoundTimeout(round) + safeTestDelay)
	require.Equal(t, int32(0), atomic.LoadInt32(&count), "callback must not fire after context cancellation")
}

func testTimeoutForRoundContextCancelledAfterArm(t *testing.T, role spectypes.RunnerRole, round specqbft.Round) {
	testBeaconConfig := setupTestBeaconConfig()

	var count int32
	onTimeout := func(round specqbft.Round) {
		atomic.AddInt32(&count, 1)
	}

	ctx, cancel := context.WithCancel(t.Context())

	timer := New(ctx, testBeaconConfig, role, 0, onTimeout)
	timer.TimeoutForRound(round)
	cancel() // cancel after arming but before timeout fires
	<-time.After(timer.RoundTimeout(round) + safeTestDelay)
	require.Equal(t, int32(0), atomic.LoadInt32(&count), "callback must not fire after context cancellation")
}

func TestNegativeTimeout(t *testing.T) {
	// Negative RoundTimeout only applies to roles that use time.Until(slotStart + offset), not the
	// round-relative roles, which return fixed positive durations regardless of the time into the slot
	// (see TestRoundTimeoutRoundRelativeRolesIgnoreSlotStart).
	roles := []spectypes.RunnerRole{
		spectypes.RoleCommittee,
		ssvtypes.RoleAggregator,
		ssvtypes.RoleSyncCommitteeContribution,
	}

	for _, role := range roles {
		t.Run(fmt.Sprintf("NegativeTimeout - %s", role), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testNegativeTimeout(t, role)
			})
		})
	}
}

func testNegativeTimeout(t *testing.T, role spectypes.RunnerRole) {
	config := *networkconfig.TestNetwork.Beacon
	config.SlotDuration = 600 * time.Millisecond
	config.GenesisTime = time.Now().Add(-10 * time.Minute)

	var count int32
	timer := New(t.Context(), &config, role, 0, func(round specqbft.Round) {
		atomic.AddInt32(&count, 1)
	})

	timeout := timer.RoundTimeout(specqbft.FirstRound)
	require.Less(t, timeout, time.Duration(0), "timeout must be negative for a late-start duty")

	timer.TimeoutForRound(specqbft.FirstRound)

	<-time.After(safeTestDelay)
	require.Equal(t, int32(1), atomic.LoadInt32(&count), "callback must fire immediately for negative timeout")
}

func TestRoundRelativeRole(t *testing.T) {
	require.True(t, RoundRelativeRole(spectypes.RoleProposer))
	require.True(t, RoundRelativeRole(spectypes.RoleEnvelopeProposer))

	for _, role := range []spectypes.RunnerRole{
		spectypes.RoleCommittee,
		ssvtypes.RoleAggregator,
		ssvtypes.RoleSyncCommitteeContribution,
		spectypes.RoleAggregatorCommittee,
	} {
		require.False(t, RoundRelativeRole(role), role)
	}
}

// TestRoundTimeoutRoundRelativeRolesIgnoreSlotStart is the regression guard for the round-relative
// roles' timers: their timeout must not depend on how far into the slot the instance starts. It
// matters most for the §6 envelope proposer, whose instance only starts after the §4 block is
// published — a slot-anchored timer would already be negative there and time round 1 out on arrival.
func TestRoundTimeoutRoundRelativeRolesIgnoreSlotStart(t *testing.T) {
	roles := []struct {
		name string
		role spectypes.RunnerRole
	}{
		{"proposer", spectypes.RoleProposer},
		{"envelope_proposer", spectypes.RoleEnvelopeProposer},
	}

	for _, rc := range roles {
		t.Run(rc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				// Slot 0 started long ago; a slot-synchronized timer would be deeply negative here.
				config := *networkconfig.TestNetwork.Beacon
				config.GenesisTime = time.Now().Add(-10 * time.Minute)
				timer := New(t.Context(), &config, rc.role, 0, func(specqbft.Round) {})

				require.Equal(t, QuickTimeout, timer.RoundTimeout(specqbft.FirstRound))
				require.Equal(t, QuickTimeout, timer.RoundTimeout(QuickTimeoutThreshold))
				require.Equal(t, SlowTimeout, timer.RoundTimeout(QuickTimeoutThreshold+1))
			})
		})
	}
}

func testTimeoutForRoundMulti(t *testing.T, role spectypes.RunnerRole) {
	testBeaconConfig := setupTestBeaconConfig()

	var count int32
	var timestamps = make([]int64, 4)
	var mu sync.Mutex

	onTimeout := func(index int) {
		atomic.AddInt32(&count, 1)
		mu.Lock()
		timestamps[index] = time.Now().UnixNano()
		mu.Unlock()
	}

	for i := 0; i < 4; i++ {
		go func(index int) {
			timer := New(t.Context(), testBeaconConfig, role, 0, func(round specqbft.Round) { onTimeout(index) })
			timer.TimeoutForRound(specqbft.FirstRound)
		}(i)
		time.Sleep(time.Millisecond * 10)
	}

	referenceTimer := New(t.Context(), testBeaconConfig, role, 0, func(specqbft.Round) {})
	expectedTimeout := referenceTimer.RoundTimeout(specqbft.FirstRound) + QuickTimeout
	<-time.After(expectedTimeout + safeTestDelay)

	require.Equal(t, int32(4), atomic.LoadInt32(&count), "All four timers should have triggered")
	mu.Lock()
	for i := 1; i < 4; i++ {
		require.InDelta(t, timestamps[0], timestamps[i], float64(safeTestDelay), "All four timers should expire nearly at the same time")
	}
	mu.Unlock()
}
