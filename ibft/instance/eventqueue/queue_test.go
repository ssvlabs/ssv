package eventqueue

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestQueue(t *testing.T) {

	t.Run("add one", func(t *testing.T) {
		q := New()

		require.Nil(t, q.Pop())
		require.True(t, q.Add(func() {}))
		require.NotNil(t, q.Pop())
		require.Nil(t, q.Pop())
	})

	t.Run("add multiple", func(t *testing.T) {
		q := New()

		require.True(t, q.Add(func() {}))
		require.True(t, q.Add(func() {}))
		require.True(t, q.Add(func() {}))
		require.True(t, q.Add(func() {}))
		require.True(t, q.Add(func() {}))
		require.NotNil(t, q.Pop())
		require.NotNil(t, q.Pop())
		require.NotNil(t, q.Pop())
		require.NotNil(t, q.Pop())
		require.NotNil(t, q.Pop())
		require.Nil(t, q.Pop())
	})

	t.Run("clear and stop", func(t *testing.T) {
		q := New()

		require.True(t, q.Add(func() {}))
		q.ClearAndStop()
		require.Nil(t, q.Pop())
		require.False(t, q.Add(func() {}))
		require.Nil(t, q.Pop())
	})

}
