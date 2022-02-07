package eventqueue

import (
	"sync"
)

// Event is a wrapper of function to execute in the event queue
type Event struct {
	exec   EventExec
	cancel func()
}

// EventExec represents some function to execute
type EventExec func()

// NewEventWithCancel creates a new instance of Event with a cancel function
func NewEventWithCancel(exec EventExec, cancel func()) Event {
	return Event{
		exec:   exec,
		cancel: cancel,
	}
}

// NewEvent creates a new instance of Event
func NewEvent(exec EventExec) Event {
	return Event{
		exec:   exec,
		cancel: nil,
	}
}

// EventQueue is the interface for managing a queue of functions
type EventQueue interface {
	Add(Event) bool
	Pop() EventExec
	ClearAndStop()
	Size() int
}

// queue thread safe implementation of EventQueue
type queue struct {
	stop  bool
	queue []Event
	lock  sync.RWMutex
}

// New returns a new instance of queue
func New() EventQueue {
	q := queue{
		queue: make([]Event, 0),
		lock:  sync.RWMutex{},
	}
	return &q
}

// Add will add an item to the queue, thread safe.
func (q *queue) Add(e Event) bool {
	q.lock.Lock()
	defer q.lock.Unlock()

	if q.stop {
		return false
	}

	q.queue = append(q.queue, e)
	return true
}

// Pop will return and delete an an item from the queue, thread safe.
func (q *queue) Pop() EventExec {
	q.lock.Lock()
	defer q.lock.Unlock()

	if q.stop {
		return nil
	}

	if e := q.pop(); e != nil {
		return e.exec
	}
	return nil
}

// ClearAndStop will clear the queue disable adding more items to it, thread safe
func (q *queue) ClearAndStop() {
	q.lock.Lock()
	defer q.lock.Unlock()

	q.stop = true

	q.drain()

	q.queue = make([]Event, 0)
}

func (q *queue) Size() int {
	q.lock.RLock()
	defer q.lock.RUnlock()
	return len(q.queue)
}

// pop will return and delete an an item from the queue, NOT thread safe
func (q *queue) pop() *Event {
	if len(q.queue) > 0 {
		ret := q.queue[0]
		q.queue = q.queue[1:len(q.queue)]
		return &ret
	}
	return nil
}

// drain will empty the queue
func (q *queue) drain() {
	for {
		e := q.pop()
		if e == nil {
			break
		}
		if e.cancel != nil {
			go e.cancel()
		}
	}
}
