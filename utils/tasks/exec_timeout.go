package tasks

import (
	"context"
	"time"

	"github.com/pkg/errors"
)

// Func is the interface of functions to trigger
type Func = func(stopper Stopper) (interface{}, error)

// funcResult is an internal struct, representing result of a function
type funcResult struct {
	res interface{}
	err error
}

// ExecWithTimeout triggers some function in the given time frame, returns true if completed
func ExecWithTimeout(ctx context.Context, fn Func, timeout time.Duration) (bool, interface{}, error) {
	c := make(chan funcResult, 1)
	stopper := newStopper()

	go func() {
		defer func() {
			if err := recover(); err != nil {
				c <- funcResult{struct{}{}, errors.Errorf("panic: %s", err)}
			}
		}()
		res, err := fn(stopper)
		c <- funcResult{res, err}
	}()

	select {
	case result := <-c:
		return true, result.res, result.err
	case <-ctx.Done(): // canceled by context
		go func() {
			stopper.stop()
		}()
		return false, struct{}{}, nil
	case <-time.After(timeout):
		go func() {
			stopper.stop()
		}()
		return false, struct{}{}, nil
	}
}
