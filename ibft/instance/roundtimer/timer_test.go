package roundtimer

import (
	"context"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
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
}

func TestRoundTimer_ResetBeforeLapsed(t *testing.T) {
	timer := New(context.Background(), zap.L())
	timer.Reset(time.Millisecond * 100)
	timer.Reset(time.Millisecond * 300)

	t1 := time.Now()
	res := <-timer.ResultChan()
	t2 := time.Since(t1)
	require.True(t, res)
	require.Greater(t, t2.Milliseconds(), (time.Millisecond * 250).Milliseconds())
}

func TestRoundTimer_Stop(t *testing.T) {
	timer := New(context.Background(), zaptest.NewLogger(t))
	timer.Reset(time.Millisecond * 500)
	go func() {
		time.Sleep(time.Millisecond * 100)
		timer.Kill()
	}()
	require.False(t, <-timer.ResultChan())
}

func TestRoundTimer_Race(t *testing.T) {
	timer := New(context.Background(), zaptest.NewLogger(t))
	timer.Reset(time.Millisecond * 4)
	go func() {
		time.Sleep(time.Millisecond * 5)
		timer.Kill()
	}()
	require.True(t, <-timer.ResultChan())
}
