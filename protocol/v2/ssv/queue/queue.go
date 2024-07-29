package queue

import (
	"context"
	"time"
)

const (
	// inboxReadFrequency is the minimum time between reads from the inbox.
	inboxReadFrequency = 1 * time.Millisecond
)

// Filter is a function that returns true if the message should be popped.
type Filter func(*SSVMessage) bool

// FilterAny returns a Filter that returns true for any message.
func FilterAny(*SSVMessage) bool { return true }

// Queue is a queue of DecodedSSVMessage with dynamic (per-pop) prioritization.
type Queue interface {
	// Push blocks until the message is pushed to the queue.
	Push(*SSVMessage)

	// TryPush returns immediately with true if the message was pushed to the queue,
	// or false if the queue is full.
	TryPush(*SSVMessage) bool

	// Pop returns and removes the next message in the queue, or blocks until a message is available.
	// When the context is canceled, Pop returns immediately with any leftover message or nil.
	Pop(context.Context, MessagePrioritizer, Filter) *SSVMessage

	// TryPop returns immediately with the next message in the queue, or nil if there is none.
	TryPop(MessagePrioritizer, Filter) *SSVMessage

	// Empty returns true if the queue is empty.
	Empty() bool

	// Len returns the number of messages in the queue.
	Len() int
}

type priorityQueue struct {
	head     *item
	inbox    chan *SSVMessage
	lastRead time.Time
}

// New returns an implementation of Queue optimized for concurrent push and sequential pop.
// Pops aren't thread-safe, so don't call Pop from multiple goroutines.
func New(capacity int) Queue {
	return &priorityQueue{
		inbox: make(chan *SSVMessage, capacity),
	}
}

// NewDefault returns an implementation of Queue optimized for concurrent push and sequential pop,
// with a capacity of 32 and a PusherDropping.
func NewDefault() Queue {
	return New(32)
}

func (q *priorityQueue) Push(msg *SSVMessage) {
	q.inbox <- msg
}

func (q *priorityQueue) TryPush(msg *SSVMessage) bool {
	select {
	case q.inbox <- msg:
		return true
	default:
		return false
	}
}

func (q *priorityQueue) TryPop(prioritizer MessagePrioritizer, filter Filter) *SSVMessage {
	// Read any pending messages from the inbox.
	q.readInbox()

	// Pop the highest priority message.
	if q.head != nil {
		return q.pop(prioritizer, filter)
	}

	return nil
}

func (q *priorityQueue) Pop(ctx context.Context, prioritizer MessagePrioritizer, filter Filter) *SSVMessage {
	// Read any pending messages from the inbox, if enough time has passed.
	// inboxReadFrequency is a tradeoff between responsiveness and computational cost,
	// since reading the inbox is more expensive than just reading the head.
	if time.Since(q.lastRead) > inboxReadFrequency {
		q.readInbox()
	}

	// Try to pop immediately.
	if q.head != nil {
		if m := q.pop(prioritizer, filter); m != nil {
			return m
		}
	}

	// Wait for a message to be pushed.
Wait:
	for {
		select {
		case msg := <-q.inbox:
			if q.head == nil {
				q.head = &item{message: msg}
			} else {
				q.head = &item{message: msg, next: q.head}
			}
			if filter(msg) {
				break Wait
			}
		case <-ctx.Done():
			break Wait
		}
	}

	// Read any messages that were pushed while waiting.
	q.readInbox()

	// Pop the highest priority message.
	if q.head != nil {
		return q.pop(prioritizer, filter)
	}

	return nil
}

func (q *priorityQueue) readInbox() {
	q.lastRead = time.Now()

	for {
		select {
		case msg := <-q.inbox:
			if q.head == nil {
				q.head = &item{message: msg}
			} else {
				q.head = &item{message: msg, next: q.head}
			}
		default:
			return
		}
	}
}

func (q *priorityQueue) pop(prioritizer MessagePrioritizer, filter Filter) *SSVMessage {
	if q.head.next == nil {
		if m := q.head.message; filter(m) {
			q.head = nil
			return m
		}
		return nil
	}

	// Remove the highest priority message and return it.
	var (
		prior   *item
		highest = q.head
		current = q.head
	)
	for {
		if prioritizer.Prior(current.next.message, highest.message) && filter(current.next.message) {
			highest = current.next
			prior = current
		}
		current = current.next
		if current.next == nil {
			break
		}
	}
	if prior == nil {
		q.head = highest.next
	} else {
		prior.next = highest.next
	}
	if filter(highest.message) {
		return highest.message
	}
	return nil
}

func (q *priorityQueue) Empty() bool {
	return q.head == nil && len(q.inbox) == 0
}

func (q *priorityQueue) Len() int {
	n := len(q.inbox)
	for i := q.head; i != nil; i = i.next {
		n++
	}
	return n
}

// item is a node in a linked list of DecodedSSVMessage.
type item struct {
	message *SSVMessage
	next    *item
}
