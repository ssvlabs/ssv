package queue

import (
	"context"
	"sync"
	"sync/atomic"
	"unsafe"
)

// AtomicPriorityQueue implements Queue, it manages a lock-free linked list of DecodedSSVMessage.
// Implemented using atomic CAS (CompareAndSwap) operations to handle multiple goroutines that add/pop messages.
type AtomicPriorityQueue struct {
	head        unsafe.Pointer
	wait        chan *DecodedSSVMessage
	waiting     bool
	waitingLock sync.Mutex
}

// New initialized a AtomicPriorityQueue with the given MessagePrioritizer.
// If prioritizer is nil, the messages will be returned in the order they were pushed.
func NewAtomic() Queue {
	// nolint
	h := unsafe.Pointer(&msgItem{})
	return &AtomicPriorityQueue{
		head: h,
		wait: make(chan *DecodedSSVMessage),
	}
}

func (q *AtomicPriorityQueue) Push(msg *DecodedSSVMessage) {
	q.waitingLock.Lock()
	waiting := q.waiting
	q.waiting = false
	q.waitingLock.Unlock()
	if waiting {
		q.wait <- msg
		return
	}

	// nolint
	n := &msgItem{value: unsafe.Pointer(msg), next: q.head}
	// nolint
	added := cas(&q.head, q.head, unsafe.Pointer(n))
	if added {
		metricMsgQRatio.WithLabelValues(msg.MsgID.String()).Inc()
	}
}

func (q *AtomicPriorityQueue) Pop(prioritizer MessagePrioritizer) *DecodedSSVMessage {
	res := q.pop(prioritizer)
	if res != nil {
		metricMsgQRatio.WithLabelValues(res.MsgID.String()).Dec()
	}
	return res
}

func (q *AtomicPriorityQueue) WaitAndPop(ctx context.Context, priority MessagePrioritizer) *DecodedSSVMessage {
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

func (q *AtomicPriorityQueue) Empty() bool {
	h := load(&q.head)
	if h == nil {
		return true
	}
	return h.Value() == nil
}

func (q *AtomicPriorityQueue) pop(prioritizer MessagePrioritizer) *DecodedSSVMessage {
	var h, beforeHighest, previous *msgItem
	currentP := q.head

	for currentP != nil {
		current := load(&currentP)
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
		currentP = current.NextP()
	}

	if h == nil {
		return nil
	}

	if beforeHighest != nil {
		cas(&beforeHighest.next, beforeHighest.NextP(), h.NextP())
	} else {
		atomic.StorePointer(&q.head, h.NextP())
	}

	return h.Value()
}

func load(p *unsafe.Pointer) *msgItem {
	return (*msgItem)(atomic.LoadPointer(p))
}

func cas(p *unsafe.Pointer, old, new unsafe.Pointer) bool {
	return atomic.CompareAndSwapPointer(p, old, new)
}

// msgItem is an item in the linked list that is used by AtomicPriorityQueue
type msgItem struct {
	value unsafe.Pointer //*DecodedSSVMessage
	next  unsafe.Pointer
}

// Value returns the underlaying value
func (i *msgItem) Value() *DecodedSSVMessage {
	return (*DecodedSSVMessage)(atomic.LoadPointer(&i.value))
}

// Next returns the next item in the list
func (i *msgItem) Next() *msgItem {
	return (*msgItem)(atomic.LoadPointer(&i.next))
}

// NextP returns the next item's pointer
func (i *msgItem) NextP() unsafe.Pointer {
	return atomic.LoadPointer(&i.next)
}
