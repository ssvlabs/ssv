package queue

import (
	"context"
	"sync"
	"sync/atomic"
)

// AtomicPointerPriorityQueue implements Queue, it manages a lock-free linked list of DecodedSSVMessage.
// Implemented using atomic CAS (CompareAndSwap) operations to handle multiple goroutines that add/pop messages.
type AtomicPointerPriorityQueue struct {
	head        atomic.Pointer[atomicPointerMsgItem]
	wait        chan *DecodedSSVMessage
	waiting     bool
	waitingLock sync.Mutex
}

// New initialized a AtomicPointerPriorityQueue with the given MessagePrioritizer.
// If prioritizer is nil, the messages will be returned in the order they were pushed.
func NewAtomicPointer() Queue {
	// nolint
	pq := &AtomicPointerPriorityQueue{}
	pq.head.Store(&atomicPointerMsgItem{})
	pq.wait = make(chan *DecodedSSVMessage)
	return pq
}

func (q *AtomicPointerPriorityQueue) Push(msg *DecodedSSVMessage) {
	q.waitingLock.Lock()
	waiting := q.waiting
	q.waiting = false
	q.waitingLock.Unlock()
	if waiting {
		q.wait <- msg
		return
	}

	n := &atomicPointerMsgItem{}
	n.value.Store(msg)
	n.next.Store(q.head.Load())

	added := q.head.CompareAndSwap(q.head.Load(), n)
	if added {
		metricMsgQRatio.WithLabelValues(msg.MsgID.String()).Inc()
	}
}

func (q *AtomicPointerPriorityQueue) Pop(prioritizer MessagePrioritizer) *DecodedSSVMessage {
	res := q.pop(prioritizer)
	if res != nil {
		metricMsgQRatio.WithLabelValues(res.MsgID.String()).Dec()
	}
	return res
}

func (q *AtomicPointerPriorityQueue) WaitAndPop(ctx context.Context, priority MessagePrioritizer) *DecodedSSVMessage {
	q.waitingLock.Lock()
	if msg := q.Pop(priority); msg != nil {
		q.waitingLock.Unlock()
		return msg
	}
	q.waiting = true
	q.waitingLock.Unlock()
	select {
	case msg := <-q.wait:
		return msg
	case <-ctx.Done():
		return nil
	}
}

func (q *AtomicPointerPriorityQueue) Empty() bool {
	h := q.head.Load()
	if h == nil {
		return true
	}
	return h.Value() == nil
}

func (q *AtomicPointerPriorityQueue) pop(prioritizer MessagePrioritizer) *DecodedSSVMessage {
	var h, beforeHighest, previous *atomicPointerMsgItem
	currentP := q.head.Load()

	for currentP != nil {
		current := currentP
		val := current.Value()
		if val != nil {
			if h == nil || prioritizer.Prior(val, h.Value()) {
				if previous != nil {
					beforeHighest = previous
				}
				h = current
			}
		}
		previous = current
		currentP = current.next.Load()
	}

	if h == nil {
		return nil
	}

	if beforeHighest != nil {
		beforeHighest.next.CompareAndSwap(beforeHighest.next.Load(), h.next.Load())
	} else {
		q.head.Store(h.next.Load())
	}

	return h.value.Load()
}

// atomicPointerMsgItem is an item in the linked list that is used by AtomicPointerPriorityQueue
type atomicPointerMsgItem struct {
	value atomic.Pointer[DecodedSSVMessage]
	next  atomic.Pointer[atomicPointerMsgItem]
}

// Value returns the underlaying value
func (i *atomicPointerMsgItem) Value() *DecodedSSVMessage {
	return i.value.Load()
}
