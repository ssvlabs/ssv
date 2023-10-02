package queue

import (
	"context"
	"sync"
	"time"
)

const (
	// inboxReadFrequency is the minimum time between reads from the inbox.
	inboxReadFrequency = 1 * time.Millisecond
)

// Filter is a function that returns true if the message should be popped.
type Filter func(*DecodedSSVMessage) bool

// FilterAny returns a Filter that returns true for any message.
func FilterAny(*DecodedSSVMessage) bool { return true }

// Queue is a queue of DecodedSSVMessage with dynamic (per-pop) prioritization.
type Queue interface {
	// Push blocks until the message is pushed to the queue.
	Push(*DecodedSSVMessage)

	// TryPush returns immediately with true if the message was pushed to the queue,
	// or false if the queue is full.
	TryPush(*DecodedSSVMessage) bool

	// Pop returns and removes the next message in the queue, or blocks until a message is available.
	// When the context is canceled, Pop returns immediately with any leftover message or nil.
	Pop(context.Context, MessagePrioritizer, Filter) *DecodedSSVMessage

	// TryPop returns immediately with the next message in the queue, or nil if there is none.
	TryPop(MessagePrioritizer, Filter) *DecodedSSVMessage

	// Empty returns true if the queue is empty.
	Empty() bool

	// Len returns the number of messages in the queue.
	Len() int
}

type priorityQueue struct {
	head     *item
	inbox    chan *DecodedSSVMessage
	lastRead time.Time
	mu       sync.Mutex
}

// New returns an implementation of Queue optimized for concurrent push and sequential pop.
// Pops aren't thread-safe, so don't call Pop from multiple goroutines.
func New(capacity int) Queue {
	return &priorityQueue{
		inbox: make(chan *DecodedSSVMessage, capacity),
	}
}

// NewDefault returns an implementation of Queue optimized for concurrent push and sequential pop,
// with a capacity of 32 and a PusherDropping.
func NewDefault() Queue {
	return New(32)
}

func (q *priorityQueue) Push(msg *DecodedSSVMessage) {
	q.inbox <- msg
}

func (q *priorityQueue) TryPush(msg *DecodedSSVMessage) bool {
	select {
	case q.inbox <- msg:
		return true
	default:
		return false
	}
}

func (q *priorityQueue) TryPop(prioritizer MessagePrioritizer, filter Filter) *DecodedSSVMessage {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Read any pending messages from the inbox.
	q.readInbox()

	// Pop the highest priority message.
	if q.head != nil {
		return q.pop(prioritizer, filter)
	}

	return nil
}

func (q *priorityQueue) Pop(ctx context.Context, prioritizer MessagePrioritizer, filter Filter) *DecodedSSVMessage {
	// Read any pending messages from the inbox, if enough time has passed.
	// inboxReadFrequency is a tradeoff between responsiveness and computational cost,
	// since reading the inbox is more expensive than just reading the head.
	q.mu.Lock()

	if time.Since(q.lastRead) > inboxReadFrequency {
		q.readInbox()
	}

	// Try to pop immediately.
	if q.head != nil {
		m := q.pop(prioritizer, filter)
		q.mu.Unlock()
		return m
	}
	q.mu.Unlock()

	// Wait for a message to be pushed.
	var receivedMsg *DecodedSSVMessage
Wait:
	select {
	case receivedMsg = <-q.inbox:
		if filter(receivedMsg) {
			break Wait
		}
	case <-ctx.Done():
		return nil
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if q.head == nil {
		q.head = &item{message: receivedMsg}
	} else {
		q.head = &item{message: receivedMsg, next: q.head}
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

func (q *priorityQueue) pop(prioritizer MessagePrioritizer, filter Filter) *DecodedSSVMessage {
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
	q.mu.Lock()
	defer q.mu.Unlock()

	return q.head == nil && len(q.inbox) == 0
}

func (q *priorityQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	n := len(q.inbox)
	for i := q.head; i != nil; i = i.next {
		n++
	}
	return n
}

// item is a node in a linked list of DecodedSSVMessage.
type item struct {
	message *DecodedSSVMessage
	next    *item
}
