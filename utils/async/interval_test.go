package async

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		i := int32(0)

		ctx, cancel := context.WithCancel(t.Context())

		Interval(ctx, time.Millisecond*10, func() {
			atomic.AddInt32(&i, 1)
		})

		// waits until Interval ticks at least once
		time.Sleep(time.Millisecond * 500)

		require.Greater(t, atomic.LoadInt32(&i), int32(1))

		cancel()

		// waits until Interval finishes
		time.Sleep(time.Millisecond * 500)

		val := atomic.LoadInt32(&i)

		// waits until Interval ticks at least once (it should no longer tick though)
		time.Sleep(time.Millisecond * 500)

		require.Equal(t, val, atomic.LoadInt32(&i))
	})
}
