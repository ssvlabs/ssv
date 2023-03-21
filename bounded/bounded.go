package bounded

import (
	"fmt"
	"runtime"
	"time"

	"go.uber.org/automaxprocs/maxprocs"
	"go.uber.org/zap"
)

var cgoroutines = runtime.NumCPU() + 1

type job struct {
	f   func() error
	out chan<- error
}

var in = make(chan job, 1024)

func init() {
	go func() {
		time.Sleep(10 * time.Second)

		zap.L().Debug("maxprocs", zap.Int("maxprocs", runtime.GOMAXPROCS(0)), zap.Int("numcpu", runtime.NumCPU()))

		_, err := maxprocs.Set(maxprocs.Logger(func(format string, args ...interface{}) {
			zap.L().Debug("maxprocs", zap.String("message", fmt.Sprintf(format, args...)))
		}))
		if err != nil {
			panic(err)
		}
	}()

	// goMaxProcs := 8
	// if goMaxProcs < runtime.NumCPU() {
	// 	goMaxProcs = runtime.NumCPU()
	// }
	// runtime.GOMAXPROCS(goMaxProcs)

	for i := 0; i < cgoroutines; i++ {
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

// Run runs f in a goroutine, bounded by cgoroutines.
func Run(f func() error) error {
	out := make(chan error)
	in <- job{f, out}
	err := <-out
	return err
}
