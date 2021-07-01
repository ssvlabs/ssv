package eventqueue

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestQueue(t *testing.T) {
	q := New()

	// add one
	require.Nil(t, q.Pop())
	q.Add(func() {})
	require.NotNil(t, q.Pop())
	require.Nil(t, q.Pop())

	// add multiple
	q.Add(func() {})
	q.Add(func() {})
	q.Add(func() {})
	q.Add(func() {})
	q.Add(func() {})
	require.NotNil(t, q.Pop())
	require.NotNil(t, q.Pop())
	require.NotNil(t, q.Pop())
	require.NotNil(t, q.Pop())
	require.NotNil(t, q.Pop())
	require.Nil(t, q.Pop())
}
