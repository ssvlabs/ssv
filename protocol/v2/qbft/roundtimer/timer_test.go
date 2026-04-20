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
		ssvtypes.RoleAggregator,
		spectypes.RoleProposer,
		ssvtypes.RoleSyncCommitteeContribution,
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
	timer := New(t.Context(), beaconConfig, role, specqbft.FirstHeight, callback)
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

	timer := New(ctx, testBeaconConfig, role, specqbft.FirstHeight, onTimeout)
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

	timer := New(ctx, testBeaconConfig, role, specqbft.FirstHeight, onTimeout)
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
	config.SlotDuration = slotDuration
	config.GenesisTime = time.Now().Add(-10 * time.Minute)

	var count int32
	timer := New(t.Context(), &config, role, specqbft.FirstHeight, func(round specqbft.Round) {
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
			timer := New(t.Context(), testBeaconConfig, role, specqbft.FirstHeight, func(round specqbft.Round) { onTimeout(index) })
			timer.timeoutOptions = TimeoutOptions{
				quickThreshold: threshold,
				quick:          quickTimeout,
			}
			timer.TimeoutForRound(specqbft.FirstRound)
		}(i)
		time.Sleep(time.Millisecond * 10)
	}

	referenceTimer := New(t.Context(), testBeaconConfig, role, specqbft.FirstHeight, func(specqbft.Round) {})
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
