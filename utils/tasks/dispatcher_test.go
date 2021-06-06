package tasks

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"sync"
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
	countMut := sync.Mutex{}
	count := 0
	n := 90
	tasks := []Task{}
	for len(tasks) < n {
		tasks = append(tasks, *NewTask(func() error {
			countMut.Lock()
			defer countMut.Unlock()
			count++
			return nil
		}, fmt.Sprintf("id-%d", len(tasks))))
	}
	go d.Start()
	for _, t := range tasks {
		d.Queue(t)
	}
	time.Sleep((100 + 20) * time.Millisecond) // 100 (expected) + 20 (buffer)
	require.Equal(t, count, n)
}