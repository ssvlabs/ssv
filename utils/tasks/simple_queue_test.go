package tasks

import (
	"github.com/stretchr/testify/require"
	"sync/atomic"
	"testing"
	"time"
)

func TestSimpleQueue(t *testing.T) {
	var i int64
	q := NewSimpleQueue(1 * time.Millisecond)

	go q.Start()

	go func() {
		count := 100
		for count > 0 {
			count--
			q.Queue(func() error {
				atomic.AddInt64(&i, 1)
				return nil
			})
		}
	}()

	go func() {
		count := 100
		for count > 0 {
			count--
			q.Queue(func() error {
				atomic.AddInt64(&i, -1)
				return nil
			})
		}
	}()

	q.Queue(func() error {
		atomic.AddInt64(&i, 1)
		return nil
	})
	q.Wait()
	require.Equal(t, int64(1), atomic.LoadInt64(&i))
	require.Equal(t, 0, len(q.(*simpleQueue).getWaiting()))
	require.Equal(t, 0, len(q.(*simpleQueue).errs))
}

func TestSimpleQueue_Stop(t *testing.T) {
	var i int64
	q := NewSimpleQueue(1 * time.Millisecond)

	go q.Start()

	q.Queue(func() error {
		atomic.AddInt64(&i, 1)
		return nil
	})
	require.Equal(t, 1, len(q.(*simpleQueue).getWaiting()))
	time.Sleep(2 * time.Millisecond)
	require.Equal(t, 0, len(q.(*simpleQueue).getWaiting()))

	require.False(t, q.(*simpleQueue).isStopped())
	q.Stop()
	require.True(t, q.(*simpleQueue).isStopped())
	q.Queue(func() error {
		atomic.AddInt64(&i, 1)
		return nil
	})
	time.Sleep(2 * time.Millisecond)
	// q was stopped, therefore the function should be kept in waiting
	require.Equal(t, 1, len(q.(*simpleQueue).getWaiting()))
	require.Equal(t, int64(1), atomic.LoadInt64(&i))
}


func TestSimpleQueue_Empty(t *testing.T) {
	q := NewSimpleQueue(1 * time.Millisecond)

	go q.Start()

	q.Wait()
	q.Stop()
	require.True(t, q.(*simpleQueue).isStopped())
}
