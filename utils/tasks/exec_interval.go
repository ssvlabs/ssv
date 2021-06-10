package tasks

import "time"

type StoppableFunc func() (bool, bool)

func ExecWithInterval(fn StoppableFunc, start, limit time.Duration) {
	interval := start
	for {
		time.Sleep(interval)

		if stop, cont := fn(); stop {
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
