// Atomic
package queue

import (
	"context"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/bloxapp/ssv-spec/types"
)

// Filter is a function that returns true if the given message should be included.
type Filter func(*DecodedSSVMessage) bool

// Pop removes a message from the queue
type Pop func()

// FilterRole returns a Filter which returns true for messages whose BeaconRole matches the given role.
func FilterRole(role types.BeaconRole) Filter {
	return func(msg *DecodedSSVMessage) bool {
		return msg != nil && msg.MsgID.GetRoleType() == role
	}
}

// Queue objective is to receive messages and pop the right msg by to specify priority.
type Queue interface {
	// Push inserts a message to the queue
	Push(*DecodedSSVMessage)
	// Pop removes & returns the highest priority message which matches the given filter.
	// Returns nil if no message is found.
	Pop(MessagePrioritizer) *DecodedSSVMessage
	// IsEmpty checks if the q is empty
	IsEmpty() bool

	// WaitAndPop waits for a message to be pushed to the queue and then returns it.
	WaitAndPop(context.Context, MessagePrioritizer) *DecodedSSVMessage
}

// PriorityQueue implements Queue, it manages a lock-free linked list of DecodedSSVMessage.
// Implemented using atomic CAS (CompareAndSwap) operations to handle multiple goroutines that add/pop messages.
type PriorityQueue struct {
	head        unsafe.Pointer
	wait        chan *DecodedSSVMessage
	waiting     bool
	waitingLock sync.Mutex
}

// New initialized a PriorityQueue with the given MessagePrioritizer.
// If prioritizer is nil, the messages will be returned in the order they were pushed.
func New() Queue {
	// nolint
	h := unsafe.Pointer(&msgItem{})
	return &PriorityQueue{
		head: h,
		wait: make(chan *DecodedSSVMessage),
	}
}

func (q *PriorityQueue) Push(msg *DecodedSSVMessage) {
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

func (q *PriorityQueue) Pop(prioritizer MessagePrioritizer) *DecodedSSVMessage {
	res := q.pop(prioritizer)
	if res != nil {
		metricMsgQRatio.WithLabelValues(res.MsgID.String()).Dec()
	}
	return res
}

func (q *PriorityQueue) WaitAndPop(ctx context.Context, priority MessagePrioritizer) *DecodedSSVMessage {
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

func (q *PriorityQueue) IsEmpty() bool {
	h := load(&q.head)
	if h == nil {
		return true
	}
	return h.Value() == nil
}

func (q *PriorityQueue) pop(prioritizer MessagePrioritizer) *DecodedSSVMessage {
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

// msgItem is an item in the linked list that is used by PriorityQueue
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

// Mutex
// package queue

// import (
// 	"sync"

// 	"github.com/bloxapp/ssv-spec/types"
// )

// // Filter is a function that returns true if the given message should be included.
// type Filter func(*DecodedSSVMessage) bool

// // Pop removes a message from the queue
// type Pop func()

// // FilterRole returns a Filter which returns true for messages whose BeaconRole matches the given role.
// func FilterRole(role types.BeaconRole) Filter {
// 	return func(msg *DecodedSSVMessage) bool {
// 		return msg != nil && msg.MsgID.GetRoleType() == role
// 	}
// }

// // Queue objective is to receive messages and pop the right msg by to specify priority.
// type Queue interface {
// 	// Push inserts a message to the queue
// 	Push(*DecodedSSVMessage)

// 	// Pop removes & returns the highest priority message which matches the given filter.
// 	// Returns nil if no message is found.
// 	Pop(MessagePrioritizer) *DecodedSSVMessage

// 	// IsEmpty checks if the q is empty
// 	IsEmpty() bool

// 	WaitAndPop(MessagePrioritizer) *DecodedSSVMessage
// }

// // PriorityQueue implements Queue, it manages a lock-free linked list of DecodedSSVMessage.
// // Implemented using atomic CAS (CompareAndSwap) operations to handle multiple goroutines that add/pop messages.
// type PriorityQueue struct {
// 	head *item
// 	lock sync.Mutex

// 	waiter  chan *DecodedSSVMessage
// 	waiting bool
// }

// // New initialized a PriorityQueue with the given MessagePrioritizer.
// // If prioritizer is nil, the messages will be returned in the order they were pushed.
// func New() Queue {
// 	return &PriorityQueue{
// 		waiter: make(chan *DecodedSSVMessage),
// 	}
// }

// func (q *PriorityQueue) Push(msg *DecodedSSVMessage) {
// 	q.lock.Lock()
// 	defer q.lock.Unlock()

// 	if q.head == nil {
// 		q.head = &item{message: msg}
// 	} else {
// 		q.head = &item{message: msg, Next: q.head}
// 	}

// 	if q.waiting {
// 		q.waiting = false
// 		q.waiter <- msg
// 	}

// 	metricMsgQRatio.WithLabelValues(msg.MsgID.String()).Inc()
// }

// func (q *PriorityQueue) Pop(prioritizer MessagePrioritizer) *DecodedSSVMessage {
// 	q.lock.Lock()
// 	defer q.lock.Unlock()

// 	if q.head == nil {
// 		return nil
// 	}
// 	return q.pop(prioritizer)
// }

// func (q *PriorityQueue) WaitAndPop(priority MessagePrioritizer) *DecodedSSVMessage {
// 	q.lock.Lock()

// 	if q.head == nil {
// 		q.waiting = true
// 		q.lock.Unlock()
// 		return <-q.waiter
// 	}

// 	defer q.lock.Unlock()
// 	return q.pop(priority)
// }

// func (q *PriorityQueue) pop(prioritizer MessagePrioritizer) *DecodedSSVMessage {
// 	if q.head.Next == nil {
// 		m := q.head.message
// 		q.head = nil
// 		return m
// 	}

// 	// Remove the highest priority message and return it.
// 	var prev, current *item
// 	current = q.head
// 	for current.Next != nil {
// 		if prioritizer.Prior(current.Next.message, current.message) {
// 			prev = current
// 			current = current.Next
// 		} else {
// 			break
// 		}
// 	}
// 	if prev == nil {
// 		q.head = current.Next
// 	} else {
// 		prev.Next = current.Next
// 	}
// 	return current.message
// }

// func (q *PriorityQueue) IsEmpty() bool {
// 	q.lock.Lock()
// 	defer q.lock.Unlock()
// 	return q.head == nil
// }

// // item is an item in the linked list that is used by PriorityQueue
// type item struct {
// 	message *DecodedSSVMessage
// 	Next    *item
// }

// Nikita's
// package queue

// import (
// 	"sync/atomic"

// 	"github.com/bloxapp/ssv-spec/types"
// )

// // Filter is a function that returns true if the given message should be included.
// type Filter func(*DecodedSSVMessage) bool

// // Pop removes a message from the queue
// type Pop func()

// // FilterRole returns a Filter which returns true for messages whose BeaconRole matches the given role.
// func FilterRole(role types.BeaconRole) Filter {
// 	return func(msg *DecodedSSVMessage) bool {
// 		return msg != nil && msg.MsgID.GetRoleType() == role
// 	}
// }

// // Queue objective is to receive messages and pop the right msg by to specify priority.
// type Queue interface {
// 	// Push inserts a message to the queue
// 	Push(*DecodedSSVMessage)
// 	// Pop removes & returns the highest priority message which matches the given filter.
// 	// Returns nil if no message is found.
// 	Pop(MessagePrioritizer) *DecodedSSVMessage
// 	// IsEmpty checks if the q is empty
// 	IsEmpty() bool
// }

// // PriorityQueue implements Queue, it manages a lock-free linked list of DecodedSSVMessage.
// // Implemented using atomic CAS (CompareAndSwap) operations to handle multiple goroutines that add/pop messages.
// type PriorityQueue struct {
// 	head atomic.Pointer[msgItem]
// }

// // New initialized a PriorityQueue with the given MessagePrioritizer.
// // If prioritizer is nil, the messages will be returned in the order they were pushed.
// func New() Queue {
// 	pq := &PriorityQueue{}
// 	pq.head.Store(&msgItem{})
// 	return pq
// }

// func (q *PriorityQueue) Push(msg *DecodedSSVMessage) {
// 	n := &msgItem{}
// 	n.value.Store(msg)
// 	n.next.Store(q.head.Load())

// 	added := q.head.CompareAndSwap(q.head.Load(), n)
// 	if added {
// 		metricMsgQRatio.WithLabelValues(msg.MsgID.String()).Inc()
// 	}
// }

// func (q *PriorityQueue) Pop(prioritizer MessagePrioritizer) *DecodedSSVMessage {
// 	res := q.pop(prioritizer)
// 	if res != nil {
// 		metricMsgQRatio.WithLabelValues(res.MsgID.String()).Dec()
// 	}
// 	return res
// }

// func (q *PriorityQueue) IsEmpty() bool {
// 	h := q.head.Load()
// 	if h == nil {
// 		return true
// 	}
// 	return h.Value() == nil
// }

// func (q *PriorityQueue) pop(prioritizer MessagePrioritizer) *DecodedSSVMessage {
// 	var h, beforeHighest, previous *msgItem
// 	currentP := q.head.Load()

// 	for currentP != nil {
// 		current := currentP
// 		val := current.Value()
// 		if val != nil {
// 			if h == nil || prioritizer.Prior(val, h.Value()) {
// 				if previous != nil {
// 					beforeHighest = previous
// 				}
// 				h = current
// 			}
// 		}
// 		previous = current
// 		currentP = current.next.Load()
// 	}

// 	if h == nil {
// 		return nil
// 	}

// 	if beforeHighest != nil {
// 		beforeHighest.next.CompareAndSwap(beforeHighest.next.Load(), h.next.Load())
// 	} else {
// 		q.head.Store(h.next.Load())
// 	}

// 	return h.value.Load()
// }

// // msgItem is an item in the linked list that is used by PriorityQueue
// type msgItem struct {
// 	value atomic.Pointer[DecodedSSVMessage]
// 	next  atomic.Pointer[msgItem]
// }

// // Value returns the underlaying value
// func (i *msgItem) Value() *DecodedSSVMessage {
// 	return i.value.Load()
// }
