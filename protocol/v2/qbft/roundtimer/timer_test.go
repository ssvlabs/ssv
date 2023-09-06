package roundtimer

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/stretchr/testify/require"
)

func TestRoundTimer_TimeoutForRound(t *testing.T) {
	t.Run("TimeoutForRound", func(t *testing.T) {
		count := int32(0)
		onTimeout := func(round specqbft.Round) {
			atomic.AddInt32(&count, 1)
		}
		timer := New(context.Background(), onTimeout)
		timer.roundTimeout = func(round specqbft.Round) time.Duration {
			return 1100 * time.Millisecond
		}
		timer.TimeoutForRound(specqbft.Round(1))
		require.Equal(t, int32(0), atomic.LoadInt32(&count))
		<-time.After(timer.roundTimeout(specqbft.Round(1)) + time.Millisecond*10)
		require.Equal(t, int32(1), atomic.LoadInt32(&count))
	})

	t.Run("timeout round before elapsed", func(t *testing.T) {
		count := int32(0)
		onTimeout := func(round specqbft.Round) {
			atomic.AddInt32(&count, 1)
		}
		timer := New(context.Background(), onTimeout)
		timer.roundTimeout = func(round specqbft.Round) time.Duration {
			return 1100 * time.Millisecond
		}

		timer.TimeoutForRound(specqbft.Round(1))
		<-time.After(timer.roundTimeout(specqbft.Round(1)) / 2)
		timer.TimeoutForRound(specqbft.Round(2)) // reset before elapsed
		require.Equal(t, int32(0), atomic.LoadInt32(&count))
		<-time.After(timer.roundTimeout(specqbft.Round(2)) + time.Millisecond*10)
		require.Equal(t, int32(1), atomic.LoadInt32(&count))
	})
}
