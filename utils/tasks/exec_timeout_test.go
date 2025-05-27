package tasks

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExecWithTimeout(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		longExec := func(stopper Stopper) (interface{}, error) {
			time.Sleep(2 * time.Millisecond)
			return true, nil
		}
		completed, res, err := ExecWithTimeout(context.TODO(), longExec, 1*time.Minute)
		require.True(t, completed)
		require.True(t, res.(bool))
		require.NoError(t, err)
	})
	t.Run("ctx timed out", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
		defer cancel()

		var started atomic.Bool

		fn := func(stopper Stopper) (interface{}, error) {
			started.Store(true)
			for {
				if stopper.IsStopped() {
					return true, nil
				}
				time.Sleep(time.Millisecond)
			}
		}
		completed, _, err := ExecWithTimeout(ctx, fn, 1*time.Hour)
		require.True(t, started.Load())
		require.False(t, completed)
		require.NoError(t, err)
	})
	t.Run("timed out", func(t *testing.T) {
		ctx := t.Context()

		var started atomic.Bool

		fn := func(stopper Stopper) (interface{}, error) {
			started.Store(true)
			for {
				if stopper.IsStopped() {
					return true, nil
				}
				time.Sleep(time.Millisecond)
			}
		}
		completed, _, err := ExecWithTimeout(ctx, fn, 10*time.Millisecond)
		require.True(t, started.Load())
		require.False(t, completed)
		require.NoError(t, err)
	})
}
