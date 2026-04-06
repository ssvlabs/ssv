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
) *RoundTimer {
	timer := New(t.Context(), beaconConfig, role)
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

	timer := setupTimer(t, testBeaconConfig, role, threshold)
	timer.OnTimeout(specqbft.FirstHeight, onTimeout)

	require.Equal(t, int32(0), atomic.LoadInt32(&count))

	timer.TimeoutForRound(specqbft.FirstHeight, threshold)
	<-time.After(timer.RoundTimeout(specqbft.FirstHeight, threshold) + safeTestDelay)
	require.Equal(t, int32(1), atomic.LoadInt32(&count))
}

func testTimeoutForRoundElapsed(t *testing.T, role spectypes.RunnerRole, threshold specqbft.Round) {
	testBeaconConfig := setupTestBeaconConfig()

	count := int32(0)
	onTimeout := func(round specqbft.Round) {
		atomic.AddInt32(&count, 1)
	}

	timer := setupTimer(t, testBeaconConfig, role, threshold)
	timer.OnTimeout(specqbft.FirstHeight, onTimeout)

	timer.TimeoutForRound(specqbft.FirstHeight, specqbft.FirstRound)
	<-time.After(timer.RoundTimeout(specqbft.FirstHeight, specqbft.FirstRound) / 2)
	timer.TimeoutForRound(specqbft.FirstHeight, specqbft.Round(2)) // reset before elapsed
	require.Equal(t, int32(0), atomic.LoadInt32(&count))
	<-time.After(timer.RoundTimeout(specqbft.FirstHeight, specqbft.Round(2)) + safeTestDelay)
	require.Equal(t, int32(1), atomic.LoadInt32(&count))
}

func testTimeoutForRoundTimerStored(t *testing.T, role spectypes.RunnerRole, threshold specqbft.Round) {
	testBeaconConfig := setupTestBeaconConfig()
	timer := setupTimer(t, testBeaconConfig, role, threshold)
	timer.OnTimeout(specqbft.FirstHeight, func(specqbft.Round) {})

	require.Nil(t, timer.timer, "timer should be nil before first call")

	timer.TimeoutForRound(specqbft.FirstHeight, threshold)

	timer.mtx.RLock()
	firstTimer := timer.timer
	timer.mtx.RUnlock()
	require.NotNil(t, firstTimer, "timer must be stored after TimeoutForRound")

	// Second call must replace the timer (stop old, create new).
	timer.TimeoutForRound(specqbft.FirstHeight, specqbft.Round(2))

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

	timer := New(ctx, testBeaconConfig, role)
	timer.timeoutOptions = TimeoutOptions{
		quickThreshold: threshold,
		quick:          quickTimeout,
		slow:           slowTimeout,
	}
	timer.OnTimeout(specqbft.FirstHeight, onTimeout)

	timer.TimeoutForRound(specqbft.FirstHeight, threshold)

	// Early return should skip timer creation entirely.
	timer.mtx.RLock()
	require.Nil(t, timer.timer, "timer must not be created when context is already canceled")
	timer.mtx.RUnlock()

	// Wait for the full round timeout to confirm no callback fires.
	<-time.After(timer.RoundTimeout(specqbft.FirstHeight, threshold) + safeTestDelay)
	require.Equal(t, int32(0), atomic.LoadInt32(&count), "callback must not fire after context cancellation")
}

func testTimeoutForRoundContextCancelledAfterArm(t *testing.T, role spectypes.RunnerRole, threshold specqbft.Round) {
	testBeaconConfig := setupTestBeaconConfig()

	var count int32
	onTimeout := func(round specqbft.Round) {
		atomic.AddInt32(&count, 1)
	}

	ctx, cancel := context.WithCancel(t.Context())

	timer := New(ctx, testBeaconConfig, role)
	timer.timeoutOptions = TimeoutOptions{
		quickThreshold: threshold,
		quick:          quickTimeout,
		slow:           slowTimeout,
	}
	timer.OnTimeout(specqbft.FirstHeight, onTimeout)

	timer.TimeoutForRound(specqbft.FirstHeight, threshold)
	cancel() // cancel after arming but before timeout fires
	<-time.After(timer.RoundTimeout(specqbft.FirstHeight, threshold) + safeTestDelay)
	require.Equal(t, int32(0), atomic.LoadInt32(&count), "callback must not fire after context cancellation")
}

func TestDeferredArming(t *testing.T) {
	roles := []spectypes.RunnerRole{
		spectypes.RoleCommittee,
		spectypes.RoleAggregator,
		spectypes.RoleProposer,
		spectypes.RoleSyncCommitteeContribution,
	}

	for _, role := range roles {
		t.Run(fmt.Sprintf("DeferredArming - %s: first duty nil callback", role), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testDeferredArming(t, role)
			})
		})

		t.Run(fmt.Sprintf("DeferredArming - %s: stale callback from previous duty", role), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testDeferredArmingWithStaleCallback(t, role)
			})
		})

		t.Run(fmt.Sprintf("DeferredArming - %s: stale timer stopped", role), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testStaleTimerStopped(t, role)
			})
		})

		t.Run(fmt.Sprintf("DeferredArming - %s: deferred replaced by new round", role), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testDeferredReplacedByNewRound(t, role)
			})
		})

		t.Run(fmt.Sprintf("DeferredArming - %s: context canceled before OnTimeout", role), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testDeferredContextCancelled(t, role)
			})
		})

		t.Run(fmt.Sprintf("DeferredArming - %s: stale call during deferred window", role), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testStaleDuringDeferredWindow(t, role)
			})
		})
	}
}

// testDeferredArming verifies the first-duty case: done=nil, TimeoutForRound defers,
// OnTimeout replays and the callback fires.
func testDeferredArming(t *testing.T, role spectypes.RunnerRole) {
	testBeaconConfig := setupTestBeaconConfig()

	// Create timer with nil callback (matches real construction in controller.go).
	timer := New(t.Context(), testBeaconConfig, role)
	timer.timeoutOptions = TimeoutOptions{
		quickThreshold: specqbft.Round(1),
		quick:          quickTimeout,
		slow:           slowTimeout,
	}

	// Arm with FirstHeight — should defer because done is nil.
	timer.TimeoutForRound(specqbft.FirstHeight, specqbft.FirstRound)

	timer.mtx.RLock()
	require.Nil(t, timer.timer, "timer must not be armed before callback is registered")
	require.NotNil(t, timer.deferred, "deferred must be stored")
	timer.mtx.RUnlock()

	// Register callback — should replay the deferred arm.
	var count int32
	timer.OnTimeout(specqbft.FirstHeight, func(round specqbft.Round) {
		atomic.AddInt32(&count, 1)
	})

	timer.mtx.RLock()
	require.NotNil(t, timer.timer, "timer must be armed after OnTimeout replay")
	timer.mtx.RUnlock()

	<-time.After(timer.RoundTimeout(specqbft.FirstHeight, specqbft.FirstRound) + safeTestDelay)
	require.Equal(t, int32(1), atomic.LoadInt32(&count), "callback must fire exactly once")
}

// testDeferredArmingWithStaleCallback verifies the duty N+1 case: done holds
// a stale callback from the previous duty, TimeoutForRound defers and stops
// the stale timer, OnTimeout replays with the correct callback.
func testDeferredArmingWithStaleCallback(t *testing.T, role spectypes.RunnerRole) {
	testBeaconConfig := setupTestBeaconConfig()

	timer := New(t.Context(), testBeaconConfig, role)
	timer.timeoutOptions = TimeoutOptions{
		quickThreshold: specqbft.Round(1),
		quick:          quickTimeout,
		slow:           slowTimeout,
	}

	// Duty N: register callback and arm normally.
	var oldCount int32
	timer.OnTimeout(specqbft.FirstHeight, func(round specqbft.Round) {
		atomic.AddInt32(&oldCount, 1)
	})
	timer.TimeoutForRound(specqbft.FirstHeight, specqbft.FirstRound)

	// Let duty N's timer fire.
	<-time.After(timer.RoundTimeout(specqbft.FirstHeight, specqbft.FirstRound) + safeTestDelay)
	require.Equal(t, int32(1), atomic.LoadInt32(&oldCount), "duty N callback must fire")

	// Duty N+1: arm with new height BEFORE registering the new callback.
	// This is the exact hazardous case — same FirstRound, different height.
	newHeight := specqbft.FirstHeight + 1
	timer.TimeoutForRound(newHeight, specqbft.FirstRound)

	timer.mtx.RLock()
	require.Nil(t, timer.timer, "timer must not be armed with stale callback")
	timer.mtx.RUnlock()

	// Register the new callback — should replay.
	var newCount int32
	timer.OnTimeout(newHeight, func(round specqbft.Round) {
		atomic.AddInt32(&newCount, 1)
	})

	<-time.After(timer.RoundTimeout(newHeight, specqbft.FirstRound) + safeTestDelay)
	require.Equal(t, int32(1), atomic.LoadInt32(&oldCount), "old callback must not fire again")
	require.Equal(t, int32(1), atomic.LoadInt32(&newCount), "new callback must fire exactly once")
}

// testStaleTimerStopped verifies that when TimeoutForRound defers for a new
// duty, it stops the running timer from the previous duty.
func testStaleTimerStopped(t *testing.T, role spectypes.RunnerRole) {
	testBeaconConfig := setupTestBeaconConfig()

	timer := New(t.Context(), testBeaconConfig, role)
	timer.timeoutOptions = TimeoutOptions{
		quickThreshold: specqbft.Round(1),
		quick:          quickTimeout,
		slow:           slowTimeout,
	}

	// Duty N: register callback and arm with a round that has a long timeout.
	var oldCount int32
	timer.OnTimeout(specqbft.FirstHeight, func(round specqbft.Round) {
		atomic.AddInt32(&oldCount, 1)
	})
	// Use threshold+1 to get the slow timeout path (longer).
	timer.TimeoutForRound(specqbft.FirstHeight, specqbft.Round(2))

	// Duty N+1: arm before the old timer fires — should stop it.
	newHeight := specqbft.FirstHeight + 1
	timer.TimeoutForRound(newHeight, specqbft.FirstRound)

	// Wait long enough for the old timer to have fired if it wasn't stopped.
	// Use the actual computed timeout (which includes slot-based base duration
	// for non-proposer roles) rather than just slowTimeout.
	<-time.After(timer.RoundTimeout(specqbft.FirstHeight, specqbft.Round(2)) + safeTestDelay)
	require.Equal(t, int32(0), atomic.LoadInt32(&oldCount), "stale timer must be stopped and not fire")
}

// testDeferredReplacedByNewRound verifies that multiple TimeoutForRound calls
// before OnTimeout result in only the latest round being replayed.
func testDeferredReplacedByNewRound(t *testing.T, role spectypes.RunnerRole) {
	testBeaconConfig := setupTestBeaconConfig()

	timer := New(t.Context(), testBeaconConfig, role)
	timer.timeoutOptions = TimeoutOptions{
		quickThreshold: specqbft.Round(2),
		quick:          quickTimeout,
		slow:           slowTimeout,
	}

	// Two deferred arms — second should overwrite the first.
	timer.TimeoutForRound(specqbft.FirstHeight, specqbft.FirstRound)
	timer.TimeoutForRound(specqbft.FirstHeight, specqbft.Round(2))

	timer.mtx.RLock()
	require.NotNil(t, timer.deferred)
	require.Equal(t, specqbft.Round(2), timer.deferred.round, "deferred must hold the latest round")
	timer.mtx.RUnlock()

	var firedRound atomic.Uint64
	var count int32
	timer.OnTimeout(specqbft.FirstHeight, func(round specqbft.Round) {
		atomic.AddInt32(&count, 1)
		firedRound.Store(uint64(round))
	})

	<-time.After(timer.RoundTimeout(specqbft.FirstHeight, specqbft.Round(2)) + safeTestDelay)
	require.Equal(t, int32(1), atomic.LoadInt32(&count), "callback must fire exactly once")
	require.Equal(t, specqbft.Round(2), specqbft.Round(firedRound.Load()), "callback must fire for the latest round")
}

// testDeferredContextCancelled verifies that if the context is canceled
// before OnTimeout is called, the deferred arm is not replayed.
func testDeferredContextCancelled(t *testing.T, role spectypes.RunnerRole) {
	testBeaconConfig := setupTestBeaconConfig()

	ctx, cancel := context.WithCancel(t.Context())

	timer := New(ctx, testBeaconConfig, role)
	timer.timeoutOptions = TimeoutOptions{
		quickThreshold: specqbft.Round(1),
		quick:          quickTimeout,
		slow:           slowTimeout,
	}

	timer.TimeoutForRound(specqbft.FirstHeight, specqbft.FirstRound)
	cancel() // Cancel before OnTimeout

	var count int32
	timer.OnTimeout(specqbft.FirstHeight, func(round specqbft.Round) {
		atomic.AddInt32(&count, 1)
	})

	// Verify no timer was armed (ctx check in OnTimeout replay).
	timer.mtx.RLock()
	require.Nil(t, timer.timer, "timer must not be armed when context is canceled")
	timer.mtx.RUnlock()

	<-time.After(quickTimeout + safeTestDelay)
	require.Equal(t, int32(0), atomic.LoadInt32(&count), "callback must not fire when context is canceled")
}

// testStaleDuringDeferredWindow verifies that a stale TimeoutForRound or OnTimeout
// for height H cannot clobber a deferred arm for height H+1 during the window
// between TimeoutForRound(H+1) and OnTimeout(H+1).
func testStaleDuringDeferredWindow(t *testing.T, role spectypes.RunnerRole) {
	testBeaconConfig := setupTestBeaconConfig()

	timer := New(t.Context(), testBeaconConfig, role)
	timer.timeoutOptions = TimeoutOptions{
		quickThreshold: specqbft.Round(1),
		quick:          quickTimeout,
		slow:           slowTimeout,
	}

	// Duty H: register callback and arm.
	timer.OnTimeout(specqbft.FirstHeight, func(specqbft.Round) {})
	timer.TimeoutForRound(specqbft.FirstHeight, specqbft.FirstRound)

	// Let duty H fire.
	<-time.After(timer.RoundTimeout(specqbft.FirstHeight, specqbft.FirstRound) + safeTestDelay)

	// Duty H+1: TimeoutForRound arrives before OnTimeout — enters deferred window.
	newHeight := specqbft.FirstHeight + 1
	timer.TimeoutForRound(newHeight, specqbft.FirstRound)

	timer.mtx.RLock()
	require.NotNil(t, timer.deferred, "must have deferred for H+1")
	require.Equal(t, newHeight, timer.deferred.height)
	timer.mtx.RUnlock()

	// Stale TimeoutForRound(H) arrives during the deferred window.
	timer.TimeoutForRound(specqbft.FirstHeight, specqbft.Round(3))

	// Deferred must still be for H+1, not overwritten by the stale call.
	timer.mtx.RLock()
	require.NotNil(t, timer.deferred, "deferred must survive stale call")
	require.Equal(t, newHeight, timer.deferred.height, "deferred must still be for H+1")
	timer.mtx.RUnlock()

	// Stale OnTimeout(H) arrives during the deferred window.
	timer.OnTimeout(specqbft.FirstHeight, func(specqbft.Round) {})

	// Deferred must still be intact.
	timer.mtx.RLock()
	require.NotNil(t, timer.deferred, "deferred must survive stale OnTimeout")
	require.Equal(t, newHeight, timer.deferred.height, "deferred must still be for H+1")
	timer.mtx.RUnlock()

	// Now the real OnTimeout(H+1) arrives — should replay.
	var count int32
	timer.OnTimeout(newHeight, func(round specqbft.Round) {
		atomic.AddInt32(&count, 1)
	})

	timer.mtx.RLock()
	require.NotNil(t, timer.timer, "timer must be armed after OnTimeout(H+1) replay")
	timer.mtx.RUnlock()

	<-time.After(timer.RoundTimeout(newHeight, specqbft.FirstRound) + safeTestDelay)
	require.Equal(t, int32(1), atomic.LoadInt32(&count), "H+1 callback must fire exactly once")
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

// testNegativeTimeout verifies that a late-start duty (where RoundTimeout
// returns a negative duration) fires the callback immediately without
// deadlocking. time.AfterFunc with a negative duration starts the callback
// goroutine right away; it blocks on RLock until the caller releases the
// write lock.
func testNegativeTimeout(t *testing.T, role spectypes.RunnerRole) {
	config := *networkconfig.TestNetwork.Beacon
	config.SlotDuration = slotDuration
	// Set genesis far in the past so RoundTimeout returns a negative duration.
	config.GenesisTime = time.Now().Add(-10 * time.Minute)

	timer := New(t.Context(), &config, role)
	timer.timeoutOptions = TimeoutOptions{
		quickThreshold: specqbft.Round(1),
		quick:          quickTimeout,
		slow:           slowTimeout,
	}

	var count int32
	timer.OnTimeout(specqbft.FirstHeight, func(round specqbft.Round) {
		atomic.AddInt32(&count, 1)
	})

	// Verify the timeout is indeed negative.
	timeout := timer.RoundTimeout(specqbft.FirstHeight, specqbft.FirstRound)
	require.Less(t, timeout, time.Duration(0), "timeout must be negative for a late-start duty")

	timer.TimeoutForRound(specqbft.FirstHeight, specqbft.FirstRound)

	// The callback should fire almost immediately (negative timeout).
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
			timer := New(t.Context(), testBeaconConfig, role)
			timer.timeoutOptions = TimeoutOptions{
				quickThreshold: threshold,
				quick:          quickTimeout,
			}
			timer.OnTimeout(specqbft.FirstHeight, func(round specqbft.Round) { onTimeout(index) })
			timer.TimeoutForRound(specqbft.FirstHeight, specqbft.FirstRound)
		}(i)
		// Introduce a sleep between creating timers to simulate a real-world scenario.
		time.Sleep(time.Millisecond * 10)
	}

	// To set up the correct expectations, we need to know when exactly this particular `role` is supposed to
	// timeout (different roles time out at different times into slot). We need to use a reference-timer for that.
	referenceTimer := New(t.Context(), testBeaconConfig, role)
	referenceTimer.timeoutOptions = TimeoutOptions{
		quickThreshold: specqbft.Round(1),
		quick:          quickTimeout,
	}
	expectedTimeout := referenceTimer.RoundTimeout(specqbft.FirstHeight, specqbft.FirstRound) + quickTimeout
	<-time.After(expectedTimeout + safeTestDelay)

	require.Equal(t, int32(4), atomic.LoadInt32(&count), "All four timers should have triggered")
	mu.Lock()
	for i := 1; i < 4; i++ {
		require.InDelta(t, timestamps[0], timestamps[i], float64(safeTestDelay), "All four timers should expire nearly at the same time")
	}
	mu.Unlock()
}
