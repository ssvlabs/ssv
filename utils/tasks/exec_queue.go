package tasks

import (
	"sync"
	"time"

	"github.com/ssvlabs/ssv/protocol/v2/queue"
)

// Fn represents a function to execute
type Fn = queue.Fn

// ExecQueueOpt enables to inject more parameters
type ExecQueueOpt func(*executionQueue)

// WithoutErrors disables errors
func WithoutErrors() ExecQueueOpt {
	return func(q *executionQueue) {
		q.errs = nil
	}
}

// executionQueue implements Queue interface
type executionQueue struct {
	waiting []Fn
	stopped bool

	wg   *sync.WaitGroup
	lock *sync.RWMutex

	visited *sync.Map

	errs []error

	interval time.Duration
}

// NewExecutionQueue creates a new instance
func NewExecutionQueue(interval time.Duration, opt ...ExecQueueOpt) queue.Queue {
	if interval.Milliseconds() == 0 {
		interval = 10 * time.Millisecond // default interval
	}
	q := executionQueue{
		waiting:  []Fn{},
		wg:       &sync.WaitGroup{},
		lock:     &sync.RWMutex{},
		visited:  &sync.Map{},
		errs:     []error{},
		interval: interval,
	}
	for _, o := range opt {
		o(&q)
	}
	return &q
}

// Stop stops the queue
func (eq *executionQueue) Stop() {
	eq.lock.Lock()
	defer eq.lock.Unlock()

	eq.stopped = true
}

// Start starts to execute events
func (eq *executionQueue) Start() {
	eq.lock.Lock()
	eq.stopped = false
	eq.lock.Unlock()

	for {
		eq.lock.Lock()
		if eq.stopped {
			eq.lock.Unlock()
			return
		}
		if len(eq.waiting) > 0 {
			next := eq.waiting[0]
			eq.waiting = eq.waiting[1:]
			eq.lock.Unlock()
			go eq.exec(next)
			continue
		}
		eq.lock.Unlock()
		time.Sleep(eq.interval)
	}
}

// QueueDistinct adds unique events to the queue
func (eq *executionQueue) QueueDistinct(fn Fn, id string) {
	if _, exist := eq.visited.Load(id); !exist {
		eq.Queue(func() error {
			err := fn()
			eq.visited.Delete(id)
			return err
		})
		eq.visited.Store(id, true)
	}
}

// Queue adds an event to the queue
func (eq *executionQueue) Queue(fn Fn) {
	eq.lock.Lock()
	defer eq.lock.Unlock()

	eq.wg.Add(1)
	eq.waiting = append(eq.waiting, fn)
}

// Wait waits until all events were executed
func (eq *executionQueue) Wait() {
	eq.wg.Wait()
}

// Errors returns the errors of events
func (eq *executionQueue) Errors() []error {
	eq.lock.RLock()
	defer eq.lock.RUnlock()

	return eq.errs
}

func (eq *executionQueue) exec(fn Fn) {
	defer eq.wg.Done()

	if err := fn(); err != nil {
		eq.lock.Lock()
		if eq.errs != nil {
			eq.errs = append(eq.errs, err)
		}
		eq.lock.Unlock()
	}
}

// getWaiting returns waiting events
func (eq *executionQueue) getWaiting() []Fn {
	eq.lock.RLock()
	defer eq.lock.RUnlock()

	return eq.waiting
}

// isStopped returns the queue state
func (eq *executionQueue) isStopped() bool {
	eq.lock.RLock()
	defer eq.lock.RUnlock()

	return eq.stopped
}
