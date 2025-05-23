package async

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInterval(t *testing.T) {
	i := int32(0)
	ctx, cancel := context.WithCancel(t.Context())
	Interval(ctx, time.Millisecond*10, func() {
		atomic.AddInt32(&i, 1)
	})
	require.Equal(t, int32(0), atomic.LoadInt32(&i))
	<-time.After(time.Millisecond * 25)
	require.Greater(t, atomic.LoadInt32(&i), int32(1))
	cancel()
	runtime.Gosched() // let the goroutine within Interval() to finish
	val := atomic.LoadInt32(&i)
	<-time.After(time.Millisecond * 25)
	require.Equal(t, val, atomic.LoadInt32(&i))
}
