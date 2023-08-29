package roundtimer

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

func TestRoundTimer_TimeoutForRound(t *testing.T) {
	t.Run("TimeoutForRound", func(t *testing.T) {
		count := int32(0)
		onTimeout := func() {
			atomic.AddInt32(&count, 1)
		}
		timer := New(context.Background(), spectypes.BNRoleAttester, spectypes.BeaconTestNetwork.EstimatedTimeAtSlot, onTimeout)
		timer.roundTimeout = func(timeAtSlotF TimeAtSlotFunc, role spectypes.BeaconRole, height specqbft.Height, round specqbft.Round) time.Duration {
			return 1100 * time.Millisecond
		}
		timer.TimeoutForRound(specqbft.FirstHeight, specqbft.Round(1))
		require.Equal(t, int32(0), atomic.LoadInt32(&count))
		<-time.After(timer.roundTimeout(timer.timeAtSlotFunc, timer.role, specqbft.FirstHeight, specqbft.Round(1)) + time.Millisecond*10)
		require.Equal(t, int32(1), atomic.LoadInt32(&count))
	})

	t.Run("timeout round before elapsed", func(t *testing.T) {
		count := int32(0)
		onTimeout := func() {
			atomic.AddInt32(&count, 1)
		}
		timer := New(context.Background(), spectypes.BNRoleAttester, spectypes.BeaconTestNetwork.EstimatedTimeAtSlot, onTimeout)
		timer.roundTimeout = func(timeAtSlotF TimeAtSlotFunc, role spectypes.BeaconRole, height specqbft.Height, round specqbft.Round) time.Duration {
			return 1100 * time.Millisecond
		}

		timer.TimeoutForRound(specqbft.FirstHeight, specqbft.Round(1))
		<-time.After(timer.roundTimeout(timer.timeAtSlotFunc, timer.role, 0, specqbft.Round(1)) / 2)
		timer.TimeoutForRound(specqbft.FirstHeight, specqbft.Round(2)) // reset before elapsed
		require.Equal(t, int32(0), atomic.LoadInt32(&count))
		<-time.After(timer.roundTimeout(timer.timeAtSlotFunc, timer.role, specqbft.FirstHeight, specqbft.Round(2)) + time.Millisecond*10)
		require.Equal(t, int32(1), atomic.LoadInt32(&count))
	})

	t.Run("timeout for first round - ATTESTER", func(t *testing.T) {
		count := int32(0)
		onTimeout := func() {
			atomic.AddInt32(&count, 1)
		}
		timeAtSlotFunc := func(slot phase0.Slot) int64 {
			return time.Now().Unix()
		}

		timer := New(context.Background(), spectypes.BNRoleAttester, timeAtSlotFunc, onTimeout)
		timer.roundTimeout = RoundTimeout
		timer.TimeoutForRound(specqbft.Height(0), specqbft.FirstRound)
		require.Equal(t, int32(0), atomic.LoadInt32(&count))
		<-time.After(timer.roundTimeout(timer.timeAtSlotFunc, timer.role, specqbft.Height(0), specqbft.FirstRound) + time.Millisecond*100)
		require.Equal(t, int32(1), atomic.LoadInt32(&count))
	})

	t.Run("timeout for first round - multiple timers", func(t *testing.T) {
		var count int32
		var timestamps = make([]int64, 4)
		var mu sync.Mutex

		onTimeout := func(index int) {
			atomic.AddInt32(&count, 1)
			mu.Lock()
			timestamps[index] = time.Now().UnixNano()
			mu.Unlock()
		}

		timeNow := time.Now()
		timeAtSlotFunc := func(slot phase0.Slot) int64 {
			return timeNow.Unix()
		}

		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func(index int) {
				timer := New(context.Background(), spectypes.BNRoleAttester, timeAtSlotFunc, func() { onTimeout(index) })
				timer.roundTimeout = RoundTimeout
				timer.TimeoutForRound(specqbft.Height(0), specqbft.FirstRound)
				wg.Done()
			}(i)
			time.Sleep(time.Millisecond * 100) // Introduce a sleep between creating timers
		}

		wg.Wait() // Wait for all go-routines to finish

		// Wait a bit more than the expected timeout to ensure all timers have triggered
		<-time.After(RoundTimeout(timeAtSlotFunc, spectypes.BNRoleAttester, specqbft.Height(0), specqbft.FirstRound) + time.Millisecond*100)

		require.Equal(t, int32(4), atomic.LoadInt32(&count), "All four timers should have triggered")

		mu.Lock()
		for i := 1; i < 4; i++ {
			require.InDelta(t, timestamps[0], timestamps[i], float64(time.Millisecond*10), "All four timers should expire nearly at the same time")
		}
		mu.Unlock()
	})
}
