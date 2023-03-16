package bounded

import (
	"runtime"
	"sync"
	"time"

	"github.com/paulbellamy/ratecounter"
	"go.uber.org/zap"
)

const goroutines = 6

type job struct {
	f   func() error
	out chan<- error
}

var in = make(chan job, goroutines)

func init() {
	runtime.GOMAXPROCS(12)

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
			zap.L().Debug("bounded cgo average time", zap.Float64("time_ms", time.Duration(runCounter.Rate()).Seconds()))
		}
	}()

	// Measure the time it takes to start a goroutine.
	go func() {
		var goroutineCounter = ratecounter.NewAvgRateCounter(60 * time.Second)
		var printRateTicker = time.NewTicker(5 * time.Second)
		var wg sync.WaitGroup
		for {
			start := time.Now()
			wg.Add(1)
			go func() {
				defer wg.Done()
				d := time.Since(start)
				goroutineCounter.Incr(int64(d))
				if d >= time.Millisecond*1 {
					zap.L().Debug("TRACE: goroutineLag", zap.Int64("time_ms", d.Milliseconds()), zap.Bool("very_long", d > time.Millisecond*20))
				}
			}()
			wg.Wait()
			time.Sleep(time.Millisecond * 2)

			select {
			case <-printRateTicker.C:
				zap.L().Debug("TRACE: avg goroutine start time", zap.Float64("time_ms", time.Duration(goroutineCounter.Rate()).Seconds()))
			default:
			}
		}
	}()

	// Measure the time it takes to send/receive on a channel.
	go func() {
		var ch = make(chan struct{})
		var channelCounter = ratecounter.NewAvgRateCounter(60 * time.Second)
		var printRateTicker = time.NewTicker(5 * time.Second)
		var wg sync.WaitGroup
		for {
			start := time.Now()
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-ch
				d := time.Since(start)
				channelCounter.Incr(int64(d))
				if d >= time.Millisecond*1 {
					zap.L().Debug("TRACE: channelLag", zap.Int64("time_ms", d.Milliseconds()), zap.Bool("very_long", d > time.Millisecond*20))
				}
			}()
			wg.Wait()
			time.Sleep(time.Millisecond * 2)

			select {
			case <-printRateTicker.C:
				zap.L().Debug("TRACE: avg channel time", zap.Float64("time_ms", time.Duration(channelCounter.Rate()).Seconds()))
			default:
			}
		}
	}()
}

var runCounter = ratecounter.NewAvgRateCounter(60 * time.Second)

// Run runs the function f in a goroutine.
func Run(f func() error) error {
	// start := time.Now()
	// defer func() {
	// 	counter.Incr(int64(time.Since(start)))
	// }()

	// out := make(chan error)
	// in <- job{f, out}
	// return <-out

	return f()
}
