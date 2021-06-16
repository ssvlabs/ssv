package tasks

import (
	"context"
	"time"
)

// Func is the interface of valid functions to trigger
type Func = func() (interface{}, error)

// funcResult is an internal struct, representing result of a function
type funcResult struct {
	Response interface{}
	Error    error
}

// ExecWithTimeout triggers some function in the given time frame, returns true if completed
func ExecWithTimeout(ctx context.Context, fn Func, t time.Duration) (bool, interface{}, error) {
	c := make(chan funcResult)

	go func() {
		res, err := fn()
		c <- funcResult{res, err}
	}()

	select {
	case result := <-c:
		return true, result.Response, result.Error
	case <-ctx.Done(): // cancelled by context
		return false, struct{}{}, nil
	case <-time.After(t):
		return false, struct{}{}, nil
	}
}
