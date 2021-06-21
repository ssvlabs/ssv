package tasks

import (
	"context"
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestExecWithTimeout(t *testing.T) {
	ctxWithTimeout, cancel := context.WithTimeout(context.TODO(), 7 * time.Millisecond)
	defer cancel()
	tests := []struct {
		name          string
		ctx           context.Context
		t             time.Duration
		expectedCount uint32
	}{
		{
			"Cancelled_context",
			ctxWithTimeout,
			12 * time.Millisecond,
			4,
		},
		{
			"Long_function",
			context.TODO(),
			7 * time.Millisecond,
			4,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var count uint32
			var stopped sync.WaitGroup

			stopped.Add(1)
			fn := func(stopper Stopper) (interface{}, error) {
				stop := stopper.Chan()
				for {
					select {
					case <-stop:
						stopped.Done()
						return true, nil
					default:
						atomic.AddUint32(&count, 1)
						time.Sleep(2 * time.Millisecond)
					}
				}
			}
			completed, _, err := ExecWithTimeout(context.TODO(), fn, 7 * time.Millisecond)
			stopped.Wait()
			require.False(t, completed)
			require.NoError(t, err)
			require.GreaterOrEqual(t, count, test.expectedCount)
		})
	}
}

func TestExecWithTimeout_ShortFunc(t *testing.T) {
	longExec := func(stopper Stopper) (interface{}, error) {
		time.Sleep(2 * time.Millisecond)
		return true, nil
	}
	completed, res, err := ExecWithTimeout(context.TODO(), longExec, 10 * time.Millisecond)
	require.True(t, completed)
	require.True(t, res.(bool))
	require.NoError(t, err)
}
