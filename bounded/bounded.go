package bounded

import (
	"runtime"
	"time"

	"github.com/paulbellamy/ratecounter"
	"go.uber.org/zap"
)

const goroutines = 4

type job struct {
	f   func() error
	out chan<- error
}

var in = make(chan job, goroutines)

func init() {
	for i := 0; i < goroutines; i++ {
		go func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			for j := range in {
				j.out <- j.f()
			}
		}()
	}

	go func() {
		t := time.NewTicker(1 * time.Second)
		for range t.C {
			zap.L().Debug("bounded goroutine average time", zap.Float64("time_ms", time.Duration(counter.Rate()).Seconds()))
		}
	}()

	// Measure the time it takes to start a goroutine.
	go func() {
		for {
			start := time.Now()
			go func() {
				d := time.Since(start)
				if d >= time.Millisecond*1 {
					zap.L().Debug("TRACE:goroutineLag", zap.Int64("tookMillis", d.Milliseconds()), zap.Bool("very_long", d > time.Millisecond*20))
				}
			}()
			time.Sleep(time.Millisecond * 100)
		}
	}()
}

var counter = ratecounter.NewAvgRateCounter(60 * time.Second)

// Run runs the function f in a goroutine.
func Run(f func() error) error {
	// start := time.Now()
	// defer func() {
	// 	counter.Incr(int64(time.Since(start)))
	// }()
	out := make(chan error)
	in <- job{f, out}
	return <-out
}
