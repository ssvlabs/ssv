package queue

import (
	"context"
	"sync"
)

// LockingPriorityQueue implements Queue, it manages a lock-free linked list of DecodedSSVMessage.
// Implemented using atomic CAS (CompareAndSwap) operations to handle multiple goroutines that add/pop messages.
type LockingPriorityQueue struct {
	head *item
	lock sync.Mutex

	waiter  chan *DecodedSSVMessage
	waiting bool
}

// New initialized a LockingPriorityQueue with the given MessagePrioritizer.
// If prioritizer is nil, the messages will be returned in the order they were pushed.
func NewLocking() Queue {
	return &LockingPriorityQueue{
		waiter: make(chan *DecodedSSVMessage),
	}
}

func (q *LockingPriorityQueue) Push(msg *DecodedSSVMessage) {
	q.lock.Lock()
	defer q.lock.Unlock()

	if q.waiting {
		q.waiting = false
		q.waiter <- msg
	}

	if q.head == nil {
		q.head = &item{message: msg}
	} else {
		q.head = &item{message: msg, next: q.head}
	}

	metricMsgQRatio.WithLabelValues(msg.MsgID.String()).Inc()
}

func (q *LockingPriorityQueue) Pop(prioritizer MessagePrioritizer) *DecodedSSVMessage {
	q.lock.Lock()

	if q.head == nil {
		q.lock.Unlock()
		return nil
	}

	defer q.lock.Unlock()
	return q.pop(prioritizer)
}

func (q *LockingPriorityQueue) WaitAndPop(ctx context.Context, priority MessagePrioritizer) *DecodedSSVMessage {
	q.lock.Lock()

	if q.head == nil {
		q.waiting = true
		q.lock.Unlock()
		select {
		case <-ctx.Done():
			return nil
		case msg := <-q.waiter:
			return msg
		}
	}

	defer q.lock.Unlock()
	return q.pop(priority)
}

func (q *LockingPriorityQueue) pop(prioritizer MessagePrioritizer) *DecodedSSVMessage {
	if q.head.next == nil {
		m := q.head.message
		q.head = nil
		return m
	}

	// Remove the highest priority message and return it.
	var prev, current *item
	current = q.head
	for current.next != nil {
		if prioritizer.Prior(current.next.message, current.message) {
			prev = current
			current = current.next
		} else {
			break
		}
	}
	if prev == nil {
		q.head = current.next
	} else {
		prev.next = current.next
	}
	return current.message
}

func (q *LockingPriorityQueue) Empty() bool {
	q.lock.Lock()
	defer q.lock.Unlock()
	return q.head == nil
}
