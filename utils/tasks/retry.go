package tasks

import "github.com/ssvlabs/ssv/protocol/v2/queue"

// Fn represents a function to execute
type Fn = queue.Fn

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
