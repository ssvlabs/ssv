package tasks

import (
	"context"
	"time"
)

// ExecWithTimeout triggers some function in the given time frame, returns true if completed
func ExecWithTimeout(ctx context.Context, fn func(), t time.Duration) bool {
	c := make(chan struct{})

	go func() {
		defer close(c)
		fn()
	}()

	select {
	case <-c:
		return true
	case <-ctx.Done(): // cancelled by context
		return false
	case <-time.After(t):
		return false
	}
}
