package bounded

import (
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
}

var counter = ratecounter.NewAvgRateCounter(60 * time.Second)

// Run runs the function f in a goroutine.
func Run(f func() error) error {
	start := time.Now()
	defer func() {
		counter.Incr(int64(time.Since(start)))
	}()
	out := make(chan error)
	in <- job{f, out}
	return <-out
}
