package tasks

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ssvlabs/ssv/logging"
	"github.com/stretchr/testify/require"
)

func TestExecWithTimeout(t *testing.T) {
	ctxWithTimeout, cancel := context.WithTimeout(context.TODO(), 10*time.Millisecond)
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
			20 * time.Millisecond,
			3,
		},
		{
			"Long_function",
			context.TODO(),
			8 * time.Millisecond,
			3,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			var count uint32
			var stopped sync.WaitGroup

			stopped.Add(1)
			fn := func(stopper Stopper) (interface{}, error) {
				for {
					if stopper.IsStopped() {
						stopped.Done()
						return true, nil
					}
					atomic.AddUint32(&count, 1)
					time.Sleep(2 * time.Millisecond)
				}
			}
			completed, _, err := ExecWithTimeout(test.ctx, logging.TestLogger(t), fn, test.t)
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
	completed, res, err := ExecWithTimeout(context.TODO(), logging.TestLogger(t), longExec, 10*time.Millisecond)
	require.True(t, completed)
	require.True(t, res.(bool))
	require.NoError(t, err)
}
