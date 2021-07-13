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

func TestRoundTimer_Stop(t *testing.T) {
	timer := New()
	timer.Reset(time.Millisecond * 500)
	timer.Stop()
	require.False(t, <-timer.ResultChan())
}
