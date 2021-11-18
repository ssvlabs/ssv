package tasks

import "context"

// Retry executes a function x times or until successful
func Retry(fn Fn, retries int) error {
	var err error
	for retries > 0 {
		if err = fn(); err == nil {
			return nil
		}
		retries--
	}
	return err
}

// RetryWithContext executes a function x times or until successful
func RetryWithContext(ctx context.Context, fn Fn, retries int) error {
	var err error
	for retries > 0 {
		if ctx.Err() != nil {
			return nil
		}
		if err = fn(); err == nil {
			return nil
		}
		retries--
	}
	return err
}
