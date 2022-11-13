package roundtimer

import (
	"context"
	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"sync/atomic"
	"testing"
	"time"
)

func TestRoundTimer_TimeoutForRound(t *testing.T) {
	count := int32(0)
	done := func() {
		atomic.AddInt32(&count, 1)
	}
	timer := New(context.Background(), zap.L(), done)
	timer.roundTimeout = DefaultRoundTimeout(1.1)
	timer.TimeoutForRound(qbft.Round(1))
	require.Equal(t, int32(0), atomic.LoadInt32(&count))
	<-time.After(timer.roundTimeout(qbft.Round(1)) + time.Millisecond*10)
	require.Equal(t, int32(1), atomic.LoadInt32(&count))

	atomic.StoreInt64(&timer.round, int64(0))
	timer.TimeoutForRound(qbft.Round(1))
	require.Equal(t, int32(1), atomic.LoadInt32(&count))
	<-time.After(timer.roundTimeout(qbft.Round(1)) + time.Millisecond*10)
	require.Equal(t, int32(2), atomic.LoadInt32(&count))
}

//
//func TestRoundTimer_ResetBeforeLapsed(t *testing.T) {
//	var t1 time.Time
//	var wg sync.WaitGroup
//	wg.Add(1)
//	done := func() {
//		defer wg.Done()
//		t2 := time.Since(t1)
//		require.Greater(t, t2.Milliseconds(), (time.Millisecond * 25).Milliseconds())
//	}
//	timer := New(context.Background(), zap.L(), done)
//	timer.Reset(time.Millisecond * 10)
//	timer.Reset(time.Millisecond * 30)
//	t1 = time.Now()
//	wg.Wait()
//}
//
//func TestRoundTimer_Race(t *testing.T) {
//	// this test checks for end cases where kill is called right before/after the timer has elapsed.
//	// in the first case, reset is called first and therefore timer should end with positive result
//	// in the second case, the timer is killed and should end with negative result
//
//	t.Run("reset_wins", func(t *testing.T) {
//		timer := New(context.Background(), zaptest.NewLogger(t))
//		var wg sync.WaitGroup
//		wg.Add(1)
//		go func() {
//			defer wg.Done()
//			timer.Reset(time.Millisecond * 4)
//		}()
//		wg.Add(1)
//		go func() {
//			defer wg.Done()
//			time.Sleep(time.Millisecond * 5)
//			timer.Kill()
//		}()
//		wg.Wait()
//		require.True(t, <-timer.ResultChan())
//	})
//
//	t.Run("kill_wins", func(t *testing.T) {
//		timer := New(context.Background(), zaptest.NewLogger(t))
//		var wg sync.WaitGroup
//		wg.Add(1)
//		go func() {
//			defer wg.Done()
//			timer.Reset(time.Millisecond * 5)
//		}()
//		wg.Add(1)
//		go func() {
//			defer wg.Done()
//			time.Sleep(time.Millisecond * 4)
//			timer.Kill()
//		}()
//		wg.Wait()
//		require.False(t, <-timer.ResultChan())
//	})
//}
