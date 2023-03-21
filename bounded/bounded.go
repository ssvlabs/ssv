package bounded

import (
	"runtime"
)

const goroutines = 3

type job struct {
	f   func() error
	out chan<- error
}

var in = make(chan job, 1024)

func init() {
	runtime.GOMAXPROCS(10)

	for i := 0; i < goroutines; i++ {
		go func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()

			for j := range in {
				err := j.f()
				j.out <- err
			}
		}()
	}
}

func Run(f func() error) error {
	out := make(chan error)
	in <- job{f, out}
	err := <-out
	return err
}
