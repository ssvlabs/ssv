package roundtimer

import (
	"context"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"sync"
	"testing"
	"time"
)

func TestRoundTimer_Reset(t *testing.T) {
	timer := New(context.Background(), zap.L())
	timer.Reset(time.Millisecond * 100)
	require.False(t, timer.Stopped())
	require.True(t, <-timer.ResultChan())
	require.True(t, timer.Stopped())

	timer.Reset(time.Millisecond * 100)
	require.False(t, timer.Stopped())
	require.True(t, <-timer.ResultChan())
	require.True(t, timer.Stopped())
}

func TestRoundTimer_ResetMultiple(t *testing.T) {
	timer := New(context.Background(), zap.L())
	timer.Reset(time.Millisecond * 100)
	require.True(t, <-timer.ResultChan())
	timer.Reset(time.Millisecond * 100)
	require.False(t, timer.Stopped())
	require.True(t, <-timer.ResultChan())
	timer.Reset(time.Millisecond * 100)
	require.True(t, <-timer.ResultChan())
	require.True(t, timer.Stopped())
}

func TestRoundTimer_ResetBeforeLapsed(t *testing.T) {
	timer := New(context.Background(), zap.L())
	timer.Reset(time.Millisecond * 100)
	timer.Reset(time.Millisecond * 300)
	t1 := time.Now()
	require.True(t, <-timer.ResultChan())
	t2 := time.Since(t1)
	require.Greater(t, t2.Milliseconds(), (time.Millisecond * 250).Milliseconds())
}

func TestRoundTimer_Kill(t *testing.T) {
	timer := New(context.Background(), zaptest.NewLogger(t))
	timer.Reset(time.Millisecond * 500)
	go func() {
		time.Sleep(time.Millisecond * 100)
		timer.Kill()
	}()
	require.False(t, <-timer.ResultChan())
	// make sure another call to reset won't start the timer
	timer.Reset(time.Millisecond * 500)
	require.True(t, timer.Stopped())
}

func TestRoundTimer_Race(t *testing.T) {
	// this test checks for end cases where kill is called right before/after the timer has elapsed.
	// in the first case, reset is called first and therefore timer should end with positive result
	// in the second case, the timer is killed and should end with negative result

	t.Run("reset_wins", func(t *testing.T) {
		timer := New(context.Background(), zaptest.NewLogger(t))
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			timer.Reset(time.Millisecond * 4)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Millisecond * 5)
			timer.Kill()
		}()
		wg.Wait()
		require.True(t, <-timer.ResultChan())
	})

	t.Run("kill_wins", func(t *testing.T) {
		timer := New(context.Background(), zaptest.NewLogger(t))
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			timer.Reset(time.Millisecond * 5)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Millisecond * 4)
			timer.Kill()
		}()
		wg.Wait()
		require.False(t, <-timer.ResultChan())
	})
}
