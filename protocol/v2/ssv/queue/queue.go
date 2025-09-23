package queue

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
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

	// queueId is used to provide additional details when recording inboxSizeMetric.
	queueId         string
	inboxSizeMetric metric.Int64Gauge
}

type Option func(*priorityQueue)

func WithInboxSizeMetric(inboxSizeMetric metric.Int64Gauge, queueId string) Option {
	return func(q *priorityQueue) {
		q.queueId = queueId
		q.inboxSizeMetric = inboxSizeMetric
	}
}

// New returns an implementation of Queue optimized for concurrent push and sequential pop.
// Pops aren't thread-safe, so don't call Pop from multiple goroutines.
func New(capacity int, opts ...Option) Queue {
	q := &priorityQueue{
		inbox: make(chan *SSVMessage, capacity),
	}

	for _, opt := range opts {
		opt(q)
	}

	return q
}

func (q *priorityQueue) Push(msg *SSVMessage) {
	q.recordInboxSize(int64(len(q.inbox)) + 1)

	q.inbox <- msg
}

func (q *priorityQueue) TryPush(msg *SSVMessage) bool {
	q.recordInboxSize(int64(len(q.inbox)) + 1)

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
		highest *item
		current = q.head
	)
	if filter(current.message) {
		highest = current
	}
	for {
		if (highest == nil || prioritizer.Prior(current.next.message, highest.message)) && filter(current.next.message) {
			highest = current.next
			prior = current
		}
		current = current.next
		if current.next == nil {
			break
		}
	}
	if highest == nil {
		return nil
	}
	if prior == nil {
		q.head = highest.next
	} else {
		prior.next = highest.next
	}
	return highest.message
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

func (q *priorityQueue) recordInboxSize(inboxSize int64) {
	if q.inboxSizeMetric == nil {
		return
	}
	q.inboxSizeMetric.Record(
		context.Background(),
		inboxSize,
		metric.WithAttributes(attribute.String("queue_id", q.queueId)),
	)
}

// item is a node in a linked list of DecodedSSVMessage.
type item struct {
	message *SSVMessage
	next    *item
}
