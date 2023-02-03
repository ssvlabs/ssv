package queue

import (
	"context"
)

// Queue is a queue of DecodedSSVMessage with dynamic (per-pop) prioritization.
type Queue interface {
	// Push inserts a message to the queue
	Push(*DecodedSSVMessage)

	// TryPop returns immediately with the next message in the queue, or nil if there is none.
	TryPop(MessagePrioritizer) *DecodedSSVMessage

	// Pop returns and removes the next message in the queue, or blocks until a message is available.
	// When the context is canceled, Pop returns immediately with any leftover message or nil.
	Pop(context.Context, MessagePrioritizer) *DecodedSSVMessage

	// Empty returns true if the queue is empty.
	Empty() bool
}

type priorityQueue struct {
	head   *item
	inbox  chan *DecodedSSVMessage
	pusher Pusher
}

// New returns an implementation of Queue optimized for concurrent push and sequential pop.
// Pops aren't thread-safe, so don't call Pop from multiple goroutines.
func New(capacity int, pusher Pusher) Queue {
	return &priorityQueue{
		pusher: pusher,
		inbox:  make(chan *DecodedSSVMessage, capacity),
	}
}

// NewDefault returns an implementation of Queue optimized for concurrent push and sequential pop,
// with a default capacity of 32 and an impatient PusherDropping.
func NewDefault() Queue {
	return New(32, PusherDropping(0))
}

func (q *priorityQueue) Push(msg *DecodedSSVMessage) {
	q.pusher(q.inbox, msg)
}

func (q *priorityQueue) TryPop(prioritizer MessagePrioritizer) *DecodedSSVMessage {
	if q.head == nil {
		return nil
	}
	return q.pop(prioritizer)
}

func (q *priorityQueue) Pop(ctx context.Context, priority MessagePrioritizer) *DecodedSSVMessage {
	// Try to pop immediately.
	if q.head != nil {
		return q.pop(priority)
	}

	// Wait for a message to be pushed.
	select {
	case msg := <-q.inbox:
		q.head = &item{message: msg}
	case <-ctx.Done():
	}

	// Consume any messages that were pushed while waiting.
	for {
		select {
		case msg := <-q.inbox:
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

func (q *priorityQueue) pop(prioritizer MessagePrioritizer) *DecodedSSVMessage {
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

func (q *priorityQueue) Empty() bool {
	return q.head == nil
}

// item is a node in a linked list of DecodedSSVMessage.
type item struct {
	message *DecodedSSVMessage
	next    *item
}
