package tasks

import (
	"sync"
	"time"
)

type Queue interface {
	Start()
	Stop()
	Queue(fn Fn)
	Wait()
	Errors() []error
}

type simpleQueue struct {
	waiting []Fn
	stopped bool

	wg sync.WaitGroup
	lock sync.RWMutex

	errs []error

	interval time.Duration
}

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

func (q *simpleQueue) Stop() {
	q.lock.Lock()
	defer q.lock.Unlock()

	q.stopped = true
}

func (q *simpleQueue) Start() {
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

func (q *simpleQueue) Queue(fn Fn) {
	q.lock.Lock()
	defer q.lock.Unlock()

	q.wg.Add(1)
	q.waiting = append(q.waiting, fn)
}

func (q *simpleQueue) Wait() {
	q.wg.Wait()
}

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
