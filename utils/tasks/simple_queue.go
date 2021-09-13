package tasks

import (
	"sync"
	"time"
)

// Queue is an interface for event queue
type Queue interface {
	Start()
	Stop()
	Queue(fn Fn)
	Wait()
	Errors() []error
}

// simpleQueue implements Queue interface
type simpleQueue struct {
	waiting []Fn
	stopped bool

	wg sync.WaitGroup
	lock sync.RWMutex

	errs []error

	interval time.Duration
}

// NewSimpleQueue creates a new instance
func NewSimpleQueue(interval time.Duration) Queue {
	if interval.Milliseconds() == 0 {
		interval = 10 * time.Millisecond // default interval
	}
	q := simpleQueue{
		waiting:  []Fn{},
		wg:       sync.WaitGroup{},
		lock:     sync.RWMutex{},
		errs:     []error{},
		interval: interval,
	}
	return &q
}

// Stop stops the queue
func (q *simpleQueue) Stop() {
	q.lock.Lock()
	defer q.lock.Unlock()

	q.stopped = true
}

// isStopped returns the queue state
func (q *simpleQueue) isStopped() bool {
	q.lock.RLock()
	defer q.lock.RUnlock()

	return q.stopped
}

// Start starts executing events
func (q *simpleQueue) Start() {
	q.lock.Lock()
	q.stopped = false
	q.lock.Unlock()

	for {
		q.lock.Lock()
		if q.stopped {
			q.lock.Unlock()
			return
		}
		if len(q.waiting) > 0 {
			next := q.waiting[0]
			q.waiting = q.waiting[1:]
			q.lock.Unlock()
			go q.exec(next)
			continue
		}
		q.lock.Unlock()
		time.Sleep(q.interval)
	}
}

// Queue adds an event to the queue
func (q *simpleQueue) Queue(fn Fn) {
	q.lock.Lock()
	defer q.lock.Unlock()

	q.wg.Add(1)
	q.waiting = append(q.waiting, fn)
}

// Wait waits until all events were executed
func (q *simpleQueue) Wait() {
	q.wg.Wait()
}

// Errors returns the errors of events
func (q *simpleQueue) Errors() []error {
	q.lock.RLock()
	defer q.lock.RUnlock()

	return q.errs
}

func (q *simpleQueue) exec(fn Fn) {
	defer q.wg.Done()

	if err := fn(); err != nil {
		q.lock.Lock()
		q.errs = append(q.errs, err)
		q.lock.Unlock()
	}
}

// getWaiting returns waiting events
func (q *simpleQueue) getWaiting() []Fn {
	q.lock.RLock()
	defer q.lock.RUnlock()

	return q.waiting
}