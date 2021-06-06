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
	completed := ExecWithTimeout(context.TODO(), longExec, 2 * time.Millisecond)
	require.False(t, completed)
	require.False(t, flag)
}

func TestExecWithTimeout_ShortFunc(t *testing.T) {
	flag := false
	longExec := func() {
		time.Sleep(2 * time.Millisecond)
		flag = true
	}
	completed := ExecWithTimeout(context.TODO(), longExec, 10 * time.Millisecond)
	require.True(t, completed)
	require.True(t, flag)
}

func TestExecWithTimeout_CancelContext(t *testing.T) {
	flag := false
	longExec := func() {
		time.Sleep(5 * time.Millisecond)
		flag = true
	}
	ctx, cancel := context.WithTimeout(context.TODO(), 2 * time.Millisecond)
	defer cancel()
	completed := ExecWithTimeout(ctx, longExec, 12 * time.Millisecond)
	require.False(t, completed)
	require.False(t, flag)
}