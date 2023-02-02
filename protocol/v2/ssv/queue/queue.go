package queue

import (
	"context"

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

	// WaitAndPop waits for a message to be pushed to the queue and returns it.
	// If the context is canceled, nil is returned.
	WaitAndPop(context.Context, MessagePrioritizer) *DecodedSSVMessage

	// TODO: rename Pop to TryPop and WaitAndPop to Pop

	// Empty returns true if the queue is empty.
	Empty() bool
}

// PriorityQueue implements Queue, it manages a lock-free linked list of DecodedSSVMessage.
// Implemented using atomic CAS (CompareAndSwap) operations to handle multiple goroutines that add/pop messages.
type PriorityQueue struct {
	head *item
	push chan *DecodedSSVMessage
}

// New initialized a PriorityQueue with the given MessagePrioritizer.
func New() Queue {
	return &PriorityQueue{
		push: make(chan *DecodedSSVMessage, 64),
	}
}

func (q *PriorityQueue) Push(msg *DecodedSSVMessage) {
	q.push <- msg
	metricMsgQRatio.WithLabelValues(msg.MsgID.String()).Inc()
}

func (q *PriorityQueue) Pop(prioritizer MessagePrioritizer) *DecodedSSVMessage {
	return nil
}

func (q *PriorityQueue) WaitAndPop(ctx context.Context, priority MessagePrioritizer) *DecodedSSVMessage {
	// Try to pop immediately.
	if q.head != nil {
		return q.pop(priority)
	}

	// Wait for a message to be pushed.
	select {
	case msg := <-q.push:
		q.head = &item{message: msg}
	case <-ctx.Done():
	}

	// Consume any messages that were pushed while waiting.
	for {
		select {
		case msg := <-q.push:
			if q.head == nil {
				q.head = &item{message: msg}
			} else {
				q.head = &item{message: msg, next: q.head}
			}
		default:
			goto done
		}
	}
done:

	// Return nil if nothing was pushed.
	if q.head == nil {
		return nil
	}

	// Pop the highest priority message.
	return q.pop(priority)
}

func (q *PriorityQueue) pop(prioritizer MessagePrioritizer) *DecodedSSVMessage {
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

func (q *PriorityQueue) Empty() bool {
	return q.head == nil
}

// item is an item in the linked list that is used by PriorityQueue
type item struct {
	message *DecodedSSVMessage
	next    *item
}
