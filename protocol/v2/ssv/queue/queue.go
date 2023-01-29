package queue

import (
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
	// If a message is already in the queue, it will be returned immediately.
	// If a message is not in the queue, the function will block until a message is pushed.
	// The returned Pop function should be called when the message is no longer needed.
	// If the message is not popped, it will be returned by the next call to WaitAndPop.
	WaitAndPop(MessagePrioritizer) *DecodedSSVMessage
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
	// nolint
	n := &msgItem{value: unsafe.Pointer(msg), next: q.head}
	// nolint
	added := cas(&q.head, q.head, unsafe.Pointer(n))
	if added {
		metricMsgQRatio.WithLabelValues(msg.MsgID.String()).Inc()
	}

	q.waitingLock.Lock()
	waiting := q.waiting
	q.waiting = false
	q.waitingLock.Unlock()
	if waiting {
		q.wait <- msg
	}
}

func (q *PriorityQueue) Pop(prioritizer MessagePrioritizer) *DecodedSSVMessage {
	res := q.pop(prioritizer)
	if res != nil {
		metricMsgQRatio.WithLabelValues(res.MsgID.String()).Dec()
	}
	return res
}

func (q *PriorityQueue) WaitAndPop(priority MessagePrioritizer) *DecodedSSVMessage {
	q.waitingLock.Lock()
	if msg := q.Pop(priority); msg != nil {
		q.waitingLock.Unlock()
		return msg
	}
	q.waiting = true
	q.waitingLock.Unlock()
	return <-q.wait
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
