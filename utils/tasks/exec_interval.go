package tasks

import "time"

// StoppableFunc represents a function that returns two boolean to help with its execution
// stop will stop the interval, while continue will make the interval value to remain the same
// if both are false, the interval will be increased (x2)
type StoppableFunc func(lastTick time.Duration) (stop bool, cont bool)

// ExecWithInterval executes a function with a dynamic interval
// the interval is getting multiplied by 2 in each round, up to the given limit and then it starts all over
// 1s > 2s > 4s > 8s ... > 1s > 2s > ...
func ExecWithInterval(fn StoppableFunc, start, limit time.Duration) {
	interval := start
	for {
		time.Sleep(interval)

		if stop, cont := fn(interval); stop {
			break
		} else if cont {
			continue
		}

		if interval < limit {
			interval *= 2
		} else {
			interval = start
		}
	}
}
