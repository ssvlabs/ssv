package eventqueue

import (
	"github.com/stretchr/testify/require"
	"sync/atomic"
	"testing"
)

func TestQueue(t *testing.T) {

	t.Run("add one", func(t *testing.T) {
		q := New()

		require.Nil(t, q.Pop())
		require.True(t, q.Add(NewEvent(func() {})))
		require.NotNil(t, q.Pop())
		require.Nil(t, q.Pop())
	})

	t.Run("add multiple", func(t *testing.T) {
		q := New()

		require.True(t, q.Add(NewEvent(func() {})))
		require.True(t, q.Add(NewEvent(func() {})))
		require.True(t, q.Add(NewEvent(func() {})))
		require.True(t, q.Add(NewEvent(func() {})))
		require.True(t, q.Add(NewEvent(func() {})))
		require.NotNil(t, q.Pop())
		require.NotNil(t, q.Pop())
		require.NotNil(t, q.Pop())
		require.NotNil(t, q.Pop())
		require.NotNil(t, q.Pop())
		require.Nil(t, q.Pop())
	})

	t.Run("clear and stop", func(t *testing.T) {
		var drained uint32
		q := New()

		cancel := func() {
			atomic.AddUint32(&drained, uint32(1))
		}
		require.True(t, q.Add(NewEventWithCancel(func() {}, cancel)))
		require.True(t, q.Add(NewEventWithCancel(func() {}, cancel)))
		require.True(t, q.Add(NewEventWithCancel(func() {}, cancel)))
		q.ClearAndStop()
		require.Nil(t, q.Pop())
		require.False(t, q.Add(NewEventWithCancel(func() {}, cancel)))
		require.Nil(t, q.Pop())
		// sleep so cancel functions will be executed
		for {
			if atomic.LoadUint32(&drained) >= uint32(3) {
				return
			}
		}
	})

}
