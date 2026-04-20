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
)

const (
	slotDuration = 600 * time.Millisecond

	quickTimeout = 300 * time.Millisecond
	slowTimeout  = 600 * time.Millisecond

	// safeTestDelay is a safety buffer (a guesstimate) to use in tests that set expectations making certain
	// assumptions regarding the go-routine scheduling.
	safeTestDelay = 100 * time.Millisecond
)

func TestTimeoutForRound(t *testing.T) {
	roles := []spectypes.RunnerRole{
		spectypes.RoleCommittee,
		spectypes.RoleAggregator,
		spectypes.RoleProposer,
		spectypes.RoleSyncCommitteeContribution,
	}

	for _, role := range roles {
		t.Run(fmt.Sprintf("TimeoutForRound - %s: <= quickTimeoutThreshold", role), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testTimeoutForRound(t, role, specqbft.Round(1))
			})
		})

		t.Run(fmt.Sprintf("TimeoutForRound - %s: > quickTimeoutThreshold", role), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testTimeoutForRound(t, role, specqbft.Round(2))
			})
		})

		t.Run(fmt.Sprintf("TimeoutForRound - %s: before elapsed", role), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testTimeoutForRoundElapsed(t, role, specqbft.Round(2))
			})
		})

		t.Run(fmt.Sprintf("TimeoutForRound - %s: timer stored and reused", role), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testTimeoutForRoundTimerStored(t, role, specqbft.Round(1))
			})
		})

		t.Run(fmt.Sprintf("TimeoutForRound - %s: context canceled before arm", role), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testTimeoutForRoundContextCancelled(t, role, specqbft.Round(1))
			})
		})

		t.Run(fmt.Sprintf("TimeoutForRound - %s: context canceled after arm", role), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testTimeoutForRoundContextCancelledAfterArm(t, role, specqbft.Round(1))
			})
		})

		// TODO: Decide if to make the proposer timeout deterministic
		// Proposer role is not tested for multiple synchronized timers since it's not deterministic
		if role == spectypes.RoleProposer {
			continue
		}

		t.Run(fmt.Sprintf("TimeoutForRound - %s: multiple synchronized timers", role), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testTimeoutForRoundMulti(t, role, specqbft.Round(1))
			})
		})
	}
}

func TestEstimatedRoundAt(t *testing.T) {
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
			role:         spectypes.RoleAggregator,
			timeIntoSlot: slotDuration / 3 * 2,
			want:         specqbft.FirstRound,
		},
		{
			name:         "sync committee contribution advances after two-third slot delay plus quick timeout",
			role:         spectypes.RoleSyncCommitteeContribution,
			timeIntoSlot: slotDuration/3*2 + QuickTimeout,
			want:         specqbft.FirstRound + 1,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EstimatedRoundAt(tc.role, testBeaconConfig.SlotDuration, tc.timeIntoSlot)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// TestRoundTimeoutOffset covers the pure helper directly, which is the single source of truth
// both RoundTimeout and EstimatedRoundAt derive from. Uses custom timeout options (not the
// package constants) so the arithmetic is obvious at a glance, and picks a slot duration
// that lands the role-derived head starts on round numbers:
//
//	proposer   → head start = 0
//	committee  → head start = 12s / 3        = 4s
//	aggregator → head start = 12s / 3 * 2    = 8s
func TestRoundTimeoutOffset(t *testing.T) {
	opts := TimeoutOptions{
		quickThreshold: 3, // small threshold makes the quick/slow split easy to reason about
		quick:          10 * time.Second,
		slow:           60 * time.Second,
	}
	const testSlotDuration = 12 * time.Second

	tt := []struct {
		name  string
		role  spectypes.RunnerRole
		round specqbft.Round
		want  time.Duration
	}{
		// Proposer (head start = 0): offset is r*quick for quick rounds.
		{name: "proposer, round 1 (first quick)", role: spectypes.RoleProposer, round: 1, want: 10 * time.Second},
		{name: "proposer, round 2", role: spectypes.RoleProposer, round: 2, want: 20 * time.Second},
		{name: "proposer, round 3 (= quickThreshold)", role: spectypes.RoleProposer, round: 3, want: 30 * time.Second},
		// First slow round: quickThreshold * quick + 1 * slow.
		{name: "proposer, round 4 (first slow)", role: spectypes.RoleProposer, round: 4, want: 30*time.Second + 60*time.Second},
		{name: "proposer, round 5", role: spectypes.RoleProposer, round: 5, want: 30*time.Second + 2*60*time.Second},

		// Committee (head start = 4s): offset starts with head start added.
		{name: "committee, round 1", role: spectypes.RoleCommittee, round: 1, want: 14 * time.Second},
		{name: "committee, round 2", role: spectypes.RoleCommittee, round: 2, want: 24 * time.Second},
		{name: "committee, round 3", role: spectypes.RoleCommittee, round: 3, want: 34 * time.Second},
		{name: "committee, round 4 (first slow)", role: spectypes.RoleCommittee, round: 4, want: 4*time.Second + 30*time.Second + 60*time.Second},

		// Aggregator (head start = 8s): covers the 2/3-slot branch.
		{name: "aggregator, round 1", role: spectypes.RoleAggregator, round: 1, want: 18 * time.Second},
		{name: "aggregator, round 3", role: spectypes.RoleAggregator, round: 3, want: 38 * time.Second},
		{name: "aggregator, round 4 (first slow)", role: spectypes.RoleAggregator, round: 4, want: 8*time.Second + 30*time.Second + 60*time.Second},

		// Sync committee contribution uses the same 2/3-slot branch as aggregator.
		{name: "sync_committee_contribution, round 1", role: spectypes.RoleSyncCommitteeContribution, round: 1, want: 18 * time.Second},
	}
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			got := opts.roundTimeoutForRound(tc.role, testSlotDuration, tc.round)
			require.Equal(t, tc.want, got)
		})
	}
}

// TestEstimatedRoundAtBoundaries exercises each round's transition boundary for every role.
// For every round r, it checks EstimatedRoundAt at:
//   - offset - 1ns → still in round r
//   - offset exactly → just advanced to round r+1
//   - offset + 1ns → still in round r+1
//
// This is the test that would have caught an off-by-one `<` vs `<=` in EstimatedRoundAt's loop,
// or a wrong starting `r` — none of which the pre-existing tests directly exercised.
func TestEstimatedRoundAtBoundaries(t *testing.T) {
	// Use a realistic slot duration (12s) rather than the timer_test package's 600ms so the
	// numbers line up with the real QuickTimeout (2s) and SlowTimeout (2m) that
	// EstimatedRoundAt uses via defaultTimeoutOptions.
	const realSlotDuration = 12 * time.Second

	roles := []struct {
		name string
		role spectypes.RunnerRole
	}{
		{"proposer", spectypes.RoleProposer},
		{"committee", spectypes.RoleCommittee},
		{"aggregator", spectypes.RoleAggregator},
		{"sync_committee_contribution", spectypes.RoleSyncCommitteeContribution},
	}

	for _, rc := range roles {
		t.Run(rc.name, func(t *testing.T) {
			// Walk rounds 1..CutOffRound+2; beyond CutOffRound we've already crossed into
			// "late message" territory but EstimatedRoundAt is still defined and should
			// keep incrementing with the same rules.
			for round := specqbft.Round(1); round <= CutOffRound+2; round++ {
				offset := defaultTimeoutOptions.roundTimeoutForRound(rc.role, realSlotDuration, round)

				// 1 ns before the boundary: round r has not yet timed out.
				got, err := EstimatedRoundAt(rc.role, realSlotDuration, offset-time.Nanosecond)
				require.NoError(t, err)
				require.Equal(t, round, got, "round %d: 1ns before boundary", round)

				// Exactly at the boundary: round r has timed out, we are now in round r+1.
				got, err = EstimatedRoundAt(rc.role, realSlotDuration, offset)
				require.NoError(t, err)
				require.Equal(t, round+1, got, "round %d: exactly at boundary", round)

				// 1 ns after the boundary: still in round r+1 (until next boundary).
				got, err = EstimatedRoundAt(rc.role, realSlotDuration, offset+time.Nanosecond)
				require.NoError(t, err)
				require.Equal(t, round+1, got, "round %d: 1ns after boundary", round)
			}
		})
	}
}

// TestEstimatedRoundAtEdgeCases covers inputs at and before slot start — the cases that the
// removed early return (`if sinceFirstRoundChange <= 0 { return FirstRound, nil }`) used to
// special-case. After the refactor the loop itself handles them; this test regression-guards
// that behavior.
func TestEstimatedRoundAtEdgeCases(t *testing.T) {
	const realSlotDuration = 12 * time.Second

	tt := []struct {
		name         string
		role         spectypes.RunnerRole
		timeIntoSlot time.Duration
	}{
		// timeIntoSlot = 0: all roles should report FirstRound.
		{name: "proposer at slot start", role: spectypes.RoleProposer, timeIntoSlot: 0},
		{name: "committee at slot start", role: spectypes.RoleCommittee, timeIntoSlot: 0},
		{name: "aggregator at slot start", role: spectypes.RoleAggregator, timeIntoSlot: 0},
		{name: "sync_contribution at slot start", role: spectypes.RoleSyncCommitteeContribution, timeIntoSlot: 0},

		// Negative timeIntoSlot (unreachable in practice — validateSlotTime catches early
		// messages — but the pure function must still be well-defined).
		{name: "proposer 1s early", role: spectypes.RoleProposer, timeIntoSlot: -time.Second},
		{name: "committee 1s early", role: spectypes.RoleCommittee, timeIntoSlot: -time.Second},
		{name: "committee 100s early", role: spectypes.RoleCommittee, timeIntoSlot: -100 * time.Second},

		// Inside the committee head start (2s into a 4s head start) — still Round 1.
		{name: "committee mid-head-start", role: spectypes.RoleCommittee, timeIntoSlot: 2 * time.Second},
		{name: "committee end of head start", role: spectypes.RoleCommittee, timeIntoSlot: realSlotDuration / 3},
		// Aggregator head start is 8s; at 7s we're still in Round 1.
		{name: "aggregator mid-head-start", role: spectypes.RoleAggregator, timeIntoSlot: 7 * time.Second},
	}
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EstimatedRoundAt(tc.role, realSlotDuration, tc.timeIntoSlot)
			require.NoError(t, err)
			require.Equal(t, specqbft.FirstRound, got)
		})
	}
}

// TestRoundTimeoutMatchesRoundTimeoutOffset is a regression guard for RoundTimeout vs the
// shared roundTimeoutOffset helper. Non-proposer RoundTimeout is defined as
//
//	time.Until(slotStart + roundTimeoutOffset(headStart, round))
//
// so with GenesisTime pinned to `time.Now()` under synctest (frozen clock), slot 0 starts
// "now" and the returned duration must exactly equal roundTimeoutOffset. If anyone changes
// RoundTimeout's math without updating roundTimeoutOffset (or vice versa), this test fails.
func TestRoundTimeoutMatchesRoundTimeoutOffset(t *testing.T) {
	// Proposer uses a relative timeout, not slot-start-based, so it's exempt from the
	// "equals roundTimeoutOffset" property. We cover non-proposer roles only.
	roles := []struct {
		name string
		role spectypes.RunnerRole
	}{
		{"committee", spectypes.RoleCommittee},
		{"aggregator", spectypes.RoleAggregator},
		{"sync_committee_contribution", spectypes.RoleSyncCommitteeContribution},
	}

	// Nest synctest inside t.Run (not the other way around) — synctest.Test disallows
	// t.Run calls inside its bubble.
	for _, rc := range roles {
		t.Run(rc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				beaconConfig := setupTestBeaconConfig()
				timer := New(t.Context(), beaconConfig, rc.role, 0, func(round specqbft.Round) {})

				for round := specqbft.Round(1); round <= CutOffRound; round++ {
					expected := defaultTimeoutOptions.roundTimeoutForRound(rc.role, beaconConfig.SlotDuration, round)
					got := timer.RoundTimeout(round)
					require.Equal(t, expected, got, "round %d", round)
				}
			})
		})
	}
}

// TestEstimatedRoundAtMatchesRoundTimeout directly cross-validates EstimatedRoundAt against
// RoundTimeout for all roles (including proposer). If someone changes the formula in either
// function without updating the other, this test fails.
//
// For non-proposer (slot-synchronized) roles, RoundTimeout at frozen time == slot start returns
// the cumulative offset directly. For proposer, RoundTimeout returns individual per-round
// durations, so we accumulate them to get the boundary at which EstimatedRoundAt should advance.
func TestEstimatedRoundAtMatchesRoundTimeout(t *testing.T) {
	roles := []struct {
		name string
		role spectypes.RunnerRole
	}{
		{"proposer", spectypes.RoleProposer},
		{"committee", spectypes.RoleCommittee},
		{"aggregator", spectypes.RoleAggregator},
		{"sync_committee_contribution", spectypes.RoleSyncCommitteeContribution},
	}

	for _, rc := range roles {
		t.Run(rc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				beaconConfig := setupTestBeaconConfig()
				timer := New(t.Context(), beaconConfig, rc.role, 0, func(round specqbft.Round) {})

				// For proposer, RoundTimeout returns per-round durations; accumulate them.
				// For other roles, RoundTimeout (at frozen time = slot start) returns
				// the cumulative offset directly.
				var cumulative time.Duration
				for round := specqbft.FirstRound; round <= CutOffRound; round++ {
					rt := timer.RoundTimeout(round)
					if rc.role == spectypes.RoleProposer {
						cumulative += rt
					} else {
						cumulative = rt
					}

					// 1 ns before the boundary: still in current round.
					got, err := EstimatedRoundAt(rc.role, beaconConfig.SlotDuration, cumulative-time.Nanosecond)
					require.NoError(t, err)
					require.Equal(t, round, got, "round %d: 1ns before boundary", round)

					// Exactly at the boundary: advanced to next round.
					got, err = EstimatedRoundAt(rc.role, beaconConfig.SlotDuration, cumulative)
					require.NoError(t, err)
					require.Equal(t, round+1, got, "round %d: at boundary", round)
				}
			})
		})
	}
}

func setupTestBeaconConfig() *networkconfig.Beacon {
	config := *networkconfig.TestNetwork.Beacon
	config.SlotDuration = slotDuration
	config.GenesisTime = time.Now()

	return &config
}

func setupTimer(
	t *testing.T,
	beaconConfig *networkconfig.Beacon,
	role spectypes.RunnerRole,
	round specqbft.Round,
	callback OnRoundTimeoutF,
) *RoundTimer {
	timer := New(t.Context(), beaconConfig, role, 0, callback)
	timer.timeoutOptions = TimeoutOptions{
		quickThreshold: round,
		quick:          quickTimeout,
		slow:           slowTimeout,
	}

	return timer
}

func testTimeoutForRound(t *testing.T, role spectypes.RunnerRole, threshold specqbft.Round) {
	testBeaconConfig := setupTestBeaconConfig()

	count := int32(0)
	onTimeout := func(round specqbft.Round) {
		atomic.AddInt32(&count, 1)
	}

	timer := setupTimer(t, testBeaconConfig, role, threshold, onTimeout)

	require.Equal(t, int32(0), atomic.LoadInt32(&count))

	timer.TimeoutForRound(threshold)
	<-time.After(timer.RoundTimeout(threshold) + safeTestDelay)
	require.Equal(t, int32(1), atomic.LoadInt32(&count))
}

func testTimeoutForRoundElapsed(t *testing.T, role spectypes.RunnerRole, threshold specqbft.Round) {
	testBeaconConfig := setupTestBeaconConfig()

	count := int32(0)
	onTimeout := func(round specqbft.Round) {
		atomic.AddInt32(&count, 1)
	}

	timer := setupTimer(t, testBeaconConfig, role, threshold, onTimeout)

	timer.TimeoutForRound(specqbft.FirstRound)
	<-time.After(timer.RoundTimeout(specqbft.FirstRound) / 2)
	timer.TimeoutForRound(specqbft.Round(2)) // reset before elapsed
	require.Equal(t, int32(0), atomic.LoadInt32(&count))
	<-time.After(timer.RoundTimeout(specqbft.Round(2)) + safeTestDelay)
	require.Equal(t, int32(1), atomic.LoadInt32(&count))
}

func testTimeoutForRoundTimerStored(t *testing.T, role spectypes.RunnerRole, threshold specqbft.Round) {
	testBeaconConfig := setupTestBeaconConfig()
	timer := setupTimer(t, testBeaconConfig, role, threshold, func(specqbft.Round) {})

	require.Nil(t, timer.timer, "timer should be nil before first call")

	timer.TimeoutForRound(threshold)

	timer.mtx.RLock()
	firstTimer := timer.timer
	timer.mtx.RUnlock()
	require.NotNil(t, firstTimer, "timer must be stored after TimeoutForRound")

	// Second call must replace the timer (stop old, create new).
	timer.TimeoutForRound(specqbft.Round(2))

	timer.mtx.RLock()
	secondTimer := timer.timer
	timer.mtx.RUnlock()
	require.NotNil(t, secondTimer, "timer must be stored after second TimeoutForRound")
	require.NotSame(t, firstTimer, secondTimer, "each call must create a new timer")
}

func testTimeoutForRoundContextCancelled(t *testing.T, role spectypes.RunnerRole, threshold specqbft.Round) {
	testBeaconConfig := setupTestBeaconConfig()

	var count int32
	onTimeout := func(round specqbft.Round) {
		atomic.AddInt32(&count, 1)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel before arming

	timer := New(ctx, testBeaconConfig, role, 0, onTimeout)
	timer.timeoutOptions = TimeoutOptions{
		quickThreshold: threshold,
		quick:          quickTimeout,
		slow:           slowTimeout,
	}

	timer.TimeoutForRound(threshold)

	// Early return should skip timer creation entirely.
	timer.mtx.RLock()
	require.Nil(t, timer.timer, "timer must not be created when context is already canceled")
	timer.mtx.RUnlock()

	// Wait for the full round timeout to confirm no callback fires.
	<-time.After(timer.RoundTimeout(threshold) + safeTestDelay)
	require.Equal(t, int32(0), atomic.LoadInt32(&count), "callback must not fire after context cancellation")
}

func testTimeoutForRoundContextCancelledAfterArm(t *testing.T, role spectypes.RunnerRole, threshold specqbft.Round) {
	testBeaconConfig := setupTestBeaconConfig()

	var count int32
	onTimeout := func(round specqbft.Round) {
		atomic.AddInt32(&count, 1)
	}

	ctx, cancel := context.WithCancel(t.Context())

	timer := New(ctx, testBeaconConfig, role, 0, onTimeout)
	timer.timeoutOptions = TimeoutOptions{
		quickThreshold: threshold,
		quick:          quickTimeout,
		slow:           slowTimeout,
	}

	timer.TimeoutForRound(threshold)
	cancel() // cancel after arming but before timeout fires
	<-time.After(timer.RoundTimeout(threshold) + safeTestDelay)
	require.Equal(t, int32(0), atomic.LoadInt32(&count), "callback must not fire after context cancellation")
}

func TestNegativeTimeout(t *testing.T) {
	// Negative RoundTimeout only applies to roles that use time.Until(slotStart + offset),
	// not proposer which returns fixed positive durations.
	roles := []spectypes.RunnerRole{
		spectypes.RoleCommittee,
		spectypes.RoleAggregator,
		spectypes.RoleSyncCommitteeContribution,
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
	config.SlotDuration = slotDuration
	config.GenesisTime = time.Now().Add(-10 * time.Minute)

	var count int32
	timer := New(t.Context(), &config, role, 0, func(round specqbft.Round) {
		atomic.AddInt32(&count, 1)
	})
	timer.timeoutOptions = TimeoutOptions{
		quickThreshold: specqbft.Round(1),
		quick:          quickTimeout,
		slow:           slowTimeout,
	}

	timeout := timer.RoundTimeout(specqbft.FirstRound)
	require.Less(t, timeout, time.Duration(0), "timeout must be negative for a late-start duty")

	timer.TimeoutForRound(specqbft.FirstRound)

	<-time.After(safeTestDelay)
	require.Equal(t, int32(1), atomic.LoadInt32(&count), "callback must fire immediately for negative timeout")
}

func testTimeoutForRoundMulti(t *testing.T, role spectypes.RunnerRole, threshold specqbft.Round) {
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
			timer.timeoutOptions = TimeoutOptions{
				quickThreshold: threshold,
				quick:          quickTimeout,
			}
			timer.TimeoutForRound(specqbft.FirstRound)
		}(i)
		time.Sleep(time.Millisecond * 10)
	}

	referenceTimer := New(t.Context(), testBeaconConfig, role, 0, func(specqbft.Round) {})
	referenceTimer.timeoutOptions = TimeoutOptions{
		quickThreshold: specqbft.Round(1),
		quick:          quickTimeout,
	}
	expectedTimeout := referenceTimer.RoundTimeout(specqbft.FirstRound) + quickTimeout
	<-time.After(expectedTimeout + safeTestDelay)

	require.Equal(t, int32(4), atomic.LoadInt32(&count), "All four timers should have triggered")
	mu.Lock()
	for i := 1; i < 4; i++ {
		require.InDelta(t, timestamps[0], timestamps[i], float64(safeTestDelay), "All four timers should expire nearly at the same time")
	}
	mu.Unlock()
}
