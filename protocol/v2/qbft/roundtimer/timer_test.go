package roundtimer

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

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
		testingNetwork := spectypes.BeaconTestNetwork
		count := int32(0)
		onTimeout := func() {
			atomic.AddInt32(&count, 1)
		}
		timer := New(context.Background(), spectypes.BNRoleAttester, testingNetwork.EstimatedTimeAtSlot, onTimeout)
		timer.roundTimeout = RoundTimeout
		currentSlot := testingNetwork.EstimatedCurrentSlot()
		timer.TimeoutForRound(specqbft.Height(currentSlot), specqbft.FirstRound)
		require.Equal(t, int32(0), atomic.LoadInt32(&count))
		<-time.After(timer.roundTimeout(timer.timeAtSlotFunc, timer.role, specqbft.Height(currentSlot), specqbft.FirstRound) + time.Millisecond*100)
		require.Equal(t, int32(1), atomic.LoadInt32(&count))
	})
}
