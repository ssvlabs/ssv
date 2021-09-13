package roundtimer

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestRoundTimer_Reset(t *testing.T) {
	timer := New()
	timer.Reset(time.Millisecond * 100)
	require.False(t, timer.Stopped())
	res := <-timer.ResultChan()
	require.True(t, res)
	require.True(t, timer.Stopped())

	timer.Reset(time.Millisecond * 100)
	require.False(t, timer.Stopped())
	res = <-timer.ResultChan()
	require.True(t, res)
	require.True(t, timer.Stopped())
}

func TestRoundTimer_ResetTwice(t *testing.T) {
	timer := New()
	timer.Reset(time.Millisecond * 100)
	<-timer.ResultChan()
	timer.Reset(time.Millisecond * 100)
	res := <-timer.ResultChan()
	require.True(t, res)
}

func TestRoundTimer_ResetBeforeLapsed(t *testing.T) {
	timer := New()
	timer.Reset(time.Millisecond * 100)
	timer.Reset(time.Millisecond * 300)

	t1 := time.Now()
	res := <-timer.ResultChan()
	t2 := time.Since(t1)
	require.True(t, res)
	require.Greater(t, t2.Milliseconds(), (time.Millisecond * 150).Milliseconds())
}

func TestRoundTimer_Stop(t *testing.T) {
	timer := New()
	timer.Reset(time.Millisecond * 500)
	go func() {
		time.Sleep(time.Millisecond * 100)
		timer.Kill()
	}()
	require.False(t, <-timer.ResultChan())
}
