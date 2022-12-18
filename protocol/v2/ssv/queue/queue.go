package queue

import (
	"container/list"
	"sync"

	"github.com/bloxapp/ssv-spec/types"
)

// Filter is a function that returns true if the given message should be included.
type Filter func(*DecodedSSVMessage) bool

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

	// Sort updates the queue's order using the given MessagePrioritizer.
	Sort(MessagePrioritizer)

	// Pop removes and returns the front message in the queue.
	Pop(Filter) *DecodedSSVMessage

	// Len returns the count of messages in the queue.
	Len() int
}

// PriorityQueue is queue of DecodedSSVMessage ordered by a MessagePrioritizer.
type PriorityQueue struct {
	prioritizer MessagePrioritizer

	// messages holds the unread messages.
	// We use container/list instead of a slice or map for
	// the low-allocation inserts & removals.
	// TODO: consider a deque instead of container/list:
	// - https://github.com/gammazero/deque
	// - https://github.com/edwingeng/deque
	messages *list.List
	mu       sync.RWMutex
}

// New initialized a PriorityQueue with the given MessagePrioritizer.
// If prioritizer is nil, the messages will be returned in the order they were pushed.
func New(prioritizer MessagePrioritizer) Queue {
	return &PriorityQueue{
		prioritizer: prioritizer,
		messages:    list.New(),
	}
}

// Sort updates the queue's MessagePrioritizer.
func (p *PriorityQueue) Sort(prioritizer MessagePrioritizer) {
	p.prioritizer = prioritizer
}

// Push inserts a message to the queue.
func (q *PriorityQueue) Push(msg *DecodedSSVMessage) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.messages.PushBack(msg)
}

// Pop removes & returns the highest priority message which matches the given filter.
// Returns nil if no message is found.
func (q *PriorityQueue) Pop(filter Filter) *DecodedSSVMessage {
	highest, highestElement := q.peek(filter)
	if highestElement != nil {
		q.mu.Lock()
		defer q.mu.Unlock()

		q.messages.Remove(highestElement)
	}
	return highest
}

func (q *PriorityQueue) peek(filter Filter) (highest *DecodedSSVMessage, highestElement *list.Element) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	for e := q.messages.Front(); e != nil; e = e.Next() {
		msg := e.Value.(*DecodedSSVMessage)
		if filter != nil && !filter(msg) {
			continue
		}
		if highest == nil || (q.prioritizer != nil && q.prioritizer.Prior(msg, highest)) {
			highest = msg
			highestElement = e
		}
	}
	return
}

// Len returns the count of messages in the queue.
func (q *PriorityQueue) Len() int {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.messages.Len()
}
