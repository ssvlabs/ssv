package tasks

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestExecWithTimeout_LongFunc(t *testing.T) {
	longExec := func() (interface{}, error) {
		time.Sleep(10 * time.Millisecond)
		return true, nil
	}
	completed, _, err := ExecWithTimeout(context.TODO(), longExec, 2*time.Millisecond)
	require.False(t, completed)
	require.NoError(t, err)
}

func TestExecWithTimeout_ShortFunc(t *testing.T) {
	longExec := func() (interface{}, error) {
		time.Sleep(2 * time.Millisecond)
		return true, nil
	}
	completed, res, err := ExecWithTimeout(context.TODO(), longExec, 10*time.Millisecond)
	require.True(t, completed)
	require.True(t, res.(bool))
	require.NoError(t, err)
}

func TestExecWithTimeout_CancelContext(t *testing.T) {
	longExec := func() (interface{}, error) {
		time.Sleep(5 * time.Millisecond)
		return true, nil
	}
	ctx, cancel := context.WithTimeout(context.TODO(), 2*time.Millisecond)
	defer cancel()
	completed, _, err := ExecWithTimeout(ctx, longExec, 12*time.Millisecond)
	require.False(t, completed)
	require.Nil(t, err)
}
