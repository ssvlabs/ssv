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
	tmap := sync.Map{}
	n := 90
	tasks := []Task{}
	for len(tasks) < n {
		tid := fmt.Sprintf("id-%d", len(tasks))
		tasks = append(tasks, *NewTask(func() error {
			tmap.Store(tid, true)
			return nil
		}, tid))
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