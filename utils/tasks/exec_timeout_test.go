package tasks

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestExecWithTimeout_LongFunc(t *testing.T) {
	flag := false
	longExec := func() {
		time.Sleep(10 * time.Millisecond)
		flag = true
	}
	completed := ExecWithTimeout(longExec, 2 * time.Millisecond, context.TODO())
	require.False(t, completed)
	require.False(t, flag)
}

func TestExecWithTimeout_ShortFunc(t *testing.T) {
	flag := false
	longExec := func() {
		time.Sleep(2 * time.Millisecond)
		flag = true
	}
	completed := ExecWithTimeout(longExec, 10 * time.Millisecond, context.TODO())
	require.True(t, completed)
	require.True(t, flag)
}

func TestExecWithTimeout_CancelContext(t *testing.T) {
	flag := false
	longExec := func() {
		time.Sleep(10 * time.Millisecond)
		flag = true
	}
	completed := ExecWithTimeout(longExec, 2 * time.Millisecond, context.TODO())
	require.False(t, completed)
	require.False(t, flag)
}