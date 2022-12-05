package queue

import (
	"container/list"

	"github.com/bloxapp/ssv-spec/types"
)

// Queue is a queue of DecodedSSVMessage.
type Queue interface {
	// Push inserts a message to the queue.
	Push(*DecodedSSVMessage)

	// Sort updates the queue's order using the given MessagePrioritizer.
	Sort(MessagePrioritizer)

	// Pop removes and returns the front message in the queue.
	Pop(types.BeaconRole) *DecodedSSVMessage

	// Len returns the count of messages in the queue.
	Len() int
}

// New initialized a PriorityQueue with the given MessagePrioritizer.
// If prioritizer is nil, the messages will be returned in the order they were pushed.
func New(prioritizer MessagePrioritizer) Queue {
	return &PriorityQueue{
		prioritizer: prioritizer,
		messages:    list.New(),
	}
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
}

// Sort updates the queue's MessagePrioritizer.
func (p *PriorityQueue) Sort(prioritizer MessagePrioritizer) {
	p.prioritizer = prioritizer
}

// Push inserts a message to the queue.
func (q *PriorityQueue) Push(msg *DecodedSSVMessage) {
	q.messages.PushBack(msg)
}

// Pop removes & returns the highest priority message with the given BeaconRole.
// Returns nil if no message is found.
func (q *PriorityQueue) Pop(role types.BeaconRole) *DecodedSSVMessage {
	var (
		highest        *DecodedSSVMessage
		highestElement *list.Element
	)
	for e := q.messages.Front(); e != nil; e = e.Next() {
		msg := e.Value.(*DecodedSSVMessage)
		if msg.MsgID.GetRoleType() != role {
			continue
		}
		if highest == nil || (q.prioritizer != nil && q.prioritizer.Prior(msg, highest)) {
			highest = msg
			highestElement = e
		}
	}
	if highestElement != nil {
		q.messages.Remove(highestElement)
	}
	return highest
}

func (q *PriorityQueue) Len() int {
	return q.messages.Len()
}
