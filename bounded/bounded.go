package bounded

import (
	"log"
	"os"
	"runtime"
	"strconv"
	"sync"
)

type job struct {
	f    func()
	done chan<- struct{}
}

var in = make(chan job, 1024)

func init() {
	// Get the number of available CPUs.
	//
	// SSV_NUM_CPU is a useful override when running in environments
	// such as Kubernetes, where the runtime.NumCPU() is wrong.
	numCPU := runtime.NumCPU()
	if os.Getenv("SSV_NUM_CPU") != "" {
		numCPU, _ = strconv.Atoi(os.Getenv("SSV_NUM_CPU"))
		if numCPU < 1 {
			numCPU = 1
		}
	}

	// Set GOMAXPROCS to the number of available CPUs, but at least 10.
	goMaxProcs := numCPU
	if goMaxProcs < 10 {
		goMaxProcs = 10
	}
	runtime.GOMAXPROCS(goMaxProcs)

	// Spawn NumCPU + 1 goroutines to do CGO calls.
	cgoroutines := numCPU + 1
	for i := 0; i < cgoroutines; i++ {
		go func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()

			for j := range in {
				j.f()
				j.done <- struct{}{}
			}
		}()
	}

	// Log the number of CPUs and CGO goroutines.
	log.Printf("tuning GOMAXPROCS and CGO goroutines (num_cpu=%d, cgoroutines=%d, GOMAXPROCS=%d)", numCPU, cgoroutines, goMaxProcs)
}

var doneChanPool = sync.Pool{
	New: func() interface{} {
		return make(chan struct{}, 1)
	},
}

// CGO runs the given function in a goroutine dedicated to CGO calls,
// and returns the error returned by the function.
//
// This helps bound the number of different goroutines that call CGO
// to a fixed number of goroutines with locked OS threads, thereby
// reducing the number of OS threads that CGO creates and destroys.
func CGO(f func()) {
	out := doneChanPool.Get().(chan struct{})
	defer doneChanPool.Put(out)

	in <- job{f, out}
	<-out
}
