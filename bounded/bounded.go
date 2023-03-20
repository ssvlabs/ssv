package bounded

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/paulbellamy/ratecounter"
	"go.uber.org/zap"
)

const goroutines = 6

type job struct {
	f   func() error
	out chan<- error
}

var in = make(chan job, 1024)

func init() {
	runtime.GOMAXPROCS(4)

	// for i := 0; i < goroutines; i++ {
	// 	go func() {
	// 		runtime.LockOSThread()
	// 		defer runtime.UnlockOSThread()

	// 		for j := range in {
	// 			err := j.f()
	// 			j.out <- err
	// 		}
	// 	}()
	// }

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
	var above1ms atomic.Uint64
	var above3ms atomic.Uint64
	var above5ms atomic.Uint64
	var above10ms atomic.Uint64
	var above20ms atomic.Uint64
	go func() {
		var ch = make(chan struct{}, 32)
		var channelCounter = ratecounter.NewAvgRateCounter(60 * time.Second)
		var printRateTicker = time.NewTicker(5 * time.Second)

		go func() {
			for {
				start := time.Now()
				<-ch
				d := time.Since(start)
				channelCounter.Incr(int64(d))
				time.Sleep(time.Millisecond * 1)

				switch {
				case d >= time.Millisecond*20:
					above20ms.Add(1)
				case d >= time.Millisecond*10:
					above10ms.Add(1)
				case d >= time.Millisecond*5:
					above5ms.Add(1)
				case d >= time.Millisecond*3:
					above3ms.Add(1)
				case d >= time.Millisecond*1:
					above1ms.Add(1)
				}
			}
		}()

		for {
			start := time.Now()
			ch <- struct{}{}
			d := time.Since(start)
			channelCounter.Incr(int64(d))
			time.Sleep(time.Millisecond * 1)

			switch {
			case d >= time.Millisecond*20:
				above20ms.Add(1)
			case d >= time.Millisecond*10:
				above10ms.Add(1)
			case d >= time.Millisecond*5:
				above5ms.Add(1)
			case d >= time.Millisecond*3:
				above3ms.Add(1)
			case d >= time.Millisecond*1:
				above1ms.Add(1)
			}

			nAbove1ms, nAbove3ms, nAbove5ms, nAbove10ms, nAbove20ms := above1ms.Load(), above3ms.Load(), above5ms.Load(), above10ms.Load(), above20ms.Load()
			select {
			case <-printRateTicker.C:
				zap.L().Debug("TRACE: avg channel time",
					zap.Float64("time_ms", time.Duration(channelCounter.Rate()).Seconds()),
					zap.Uint64("calls_total", nAbove1ms),
					zap.String("above_3ms", fmt.Sprintf("%.2f%%", float64(nAbove3ms+nAbove5ms+nAbove10ms+nAbove20ms)/float64(nAbove1ms)*100)),
					zap.String("above_5ms", fmt.Sprintf("%.2f%%", float64(nAbove5ms+nAbove10ms+nAbove20ms)/float64(nAbove1ms)*100)),
					zap.String("above_10ms", fmt.Sprintf("%.2f%%", float64(nAbove10ms+nAbove20ms)/float64(nAbove1ms)*100)),
					zap.String("above_20ms", fmt.Sprintf("%.2f%%", float64(nAbove20ms)/float64(nAbove1ms)*100)),
				)
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
	// err := <-out
	// return err

	err := f()
	// runtime.Gosched()
	return err
}
