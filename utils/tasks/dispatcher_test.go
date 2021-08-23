package tasks

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewDispatcher(t *testing.T) {
	d := NewDispatcher(DispatcherOptions{
		Ctx:        context.TODO(),
		Logger:     zap.L(),
		Interval:   1 * time.Millisecond,
		Concurrent: 10,
	})
	tmap := sync.Map{}
	n := 90
	tasks := []Task{}
	for len(tasks) < n {
		tid := fmt.Sprintf("id-%d", len(tasks))
		tasks = append(tasks, *NewTask(func() error {
			tmap.Store(tid, true)
			return nil
		}, tid, nil))
	}
	go d.Start()
	for _, t := range tasks {
		d.Queue(t)
	}
	time.Sleep((100 + 20) * time.Millisecond) // 100 (expected) + 20 (buffer)
	count := 0
	tmap.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	require.Equal(t, count, n)
}

func TestTask_End(t *testing.T) {
	var i int64
	inc := func() error {
		atomic.AddInt64(&i, 1)
		return nil
	}
	t1 := NewTask(inc, "1", func() {
		atomic.AddInt64(&i, -1)
	})
	t2 := NewTask(inc, "2", func() {
		require.Equal(t, i, 1)
	})

	d := NewDispatcher(DispatcherOptions{
		Ctx:        context.TODO(),
		Logger:     zap.L(),
		Interval:   2 * time.Millisecond,
		Concurrent: 10,
	})

	d.Queue(*t1)
	d.Queue(*t2)
}
