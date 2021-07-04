package eventqueue

import "sync"

// Queue is a simple struct that manages a queue of functions. Thread safe
type Queue struct {
	stop  bool
	queue []func()
	lock  sync.Mutex
}

// New returns a new instance of Queue
func New() *Queue {
	return &Queue{
		queue: make([]func(), 0),
		lock:  sync.Mutex{},
	}
}

// Add will add an an item to the queue, thread safe.
func (q *Queue) Add(f func()) {
	q.lock.Lock()
	defer q.lock.Unlock()

	if q.stop {
		return
	}

	q.queue = append(q.queue, f)
}

// Pop will return and delete an an item from the queue, thread safe.
func (q *Queue) Pop() func() {
	q.lock.Lock()
	defer q.lock.Unlock()

	if q.stop {
		return nil
	}

	if len(q.queue) > 0 {
		ret := q.queue[0]
		q.queue = q.queue[1:len(q.queue)]
		return ret
	}
	return nil
}

// ClearAndStop will clear the queue disable adding more items to it, thread safe.
func (q *Queue) ClearAndStop() {
	q.lock.Lock()
	defer q.lock.Unlock()

	q.stop = true
	q.queue = make([]func(), 0)
}
