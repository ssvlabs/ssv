package queue

import (
	"github.com/bloxapp/ssv-spec/types"
	"sync/atomic"
	"unsafe"
)

// Filter is a function that returns true if the given message should be included.
type Filter func(*DecodedSSVMessage) bool

func combineFilters(filters ...Filter) Filter {
	return func(msg *DecodedSSVMessage) bool {
		if msg == nil {
			return false
		}
		for _, f := range filters {
			if !f(msg) {
				return false
			}
		}
		return true
	}
}

// Pop removes a message from the queue
type Pop func()

// FilterRole returns a Filter which returns true for messages whose BeaconRole matches the given role.
func FilterRole(role types.BeaconRole) Filter {
	return func(msg *DecodedSSVMessage) bool {
		return msg.MsgID.GetRoleType() == role
	}
}

// Queue is a queue of DecodedSSVMessage.
type Queue interface {
	// Push inserts a message to the queue.
	Push(*DecodedSSVMessage)
	// Pop removes and returns the front message in the queue.
	Pop(MessagePrioritizer, ...Filter) *DecodedSSVMessage
	// IsEmpty checks if the q is empty
	IsEmpty() bool
}

// PriorityQueue is queue of DecodedSSVMessage ordered by a MessagePrioritizer.
type PriorityQueue struct {
	head unsafe.Pointer
}

// New initialized a PriorityQueue with the given MessagePrioritizer.
// If prioritizer is nil, the messages will be returned in the order they were pushed.
func New() Queue {
	h := unsafe.Pointer(&msgItem{})
	return &PriorityQueue{
		head: h,
	}
}

// Push inserts a message to the queue.
func (q *PriorityQueue) Push(msg *DecodedSSVMessage) {
	n := &msgItem{value: unsafe.Pointer(msg), next: q.head}
	added := cas(&q.head, q.head, unsafe.Pointer(n))
	if added {
		metricMsgQRatio.WithLabelValues(msg.MsgID.String()).Inc()
	}
}

// Pop removes & returns the highest priority message which matches the given filter.
// Returns nil if no message is found.
func (q *PriorityQueue) Pop(prioritizer MessagePrioritizer, filters ...Filter) *DecodedSSVMessage {
	res := q.pop(prioritizer, filters...)
	if res != nil {
		metricMsgQRatio.WithLabelValues(res.MsgID.String()).Dec()
	}
	return res
}

func (q *PriorityQueue) IsEmpty() bool {
	h := load(&q.head)
	if h == nil {
		return true
	}
	return h.Value() == nil
}

func (q *PriorityQueue) pop(prioritizer MessagePrioritizer, filters ...Filter) *DecodedSSVMessage {
	var h, beforeHighest, previous *msgItem
	currentP := q.head

	filter := combineFilters(filters...)

	for currentP != nil {
		current := load(&currentP)
		val := current.Value()
		if h == nil || (filter(val) && prioritizer.Prior(val, h.Value())) {
			if previous != nil {
				beforeHighest = previous
			}
			h = current
		}
		previous = current
		currentP = current.NextP()
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

type msgItem struct {
	value unsafe.Pointer //*DecodedSSVMessage
	next  unsafe.Pointer
}

func (i *msgItem) Value() *DecodedSSVMessage {
	return (*DecodedSSVMessage)(atomic.LoadPointer(&i.value))
}

func (i *msgItem) Next() *msgItem {
	return (*msgItem)(atomic.LoadPointer(&i.next))
}

func (i *msgItem) NextP() unsafe.Pointer {
	return atomic.LoadPointer(&i.next)
}
