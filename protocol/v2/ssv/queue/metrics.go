package queue

import (
	"context"
	"time"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/jellydator/ttlcache/v3"
)

const defaultTTL = 1 * time.Minute

// Metrics records metrics about the Queue.
type Metrics interface {
	IncomingQueueMessage(messageID spectypes.MessageID)
	OutgoingQueueMessage(messageID spectypes.MessageID)
	DroppedQueueMessage(messageID spectypes.MessageID)
	MessageQueueSize(size int)
	MessageQueueCapacity(size int)
	MessageTimeInQueue(messageID spectypes.MessageID, d time.Duration)
}

type queueWithMetrics struct {
	Queue
	metrics            Metrics
	messageReceiptTime *ttlcache.Cache[spectypes.MessageID, time.Time]
}

// WithMetrics returns a wrapping of the given Queue that records metrics using the given Metrics.
func WithMetrics(q Queue, metrics Metrics) Queue {
	qm := &queueWithMetrics{
		Queue:   q,
		metrics: metrics,
		messageReceiptTime: ttlcache.New(
			ttlcache.WithTTL[spectypes.MessageID, time.Time](defaultTTL),
		),
	}

	go qm.messageReceiptTime.Start()

	return qm
}

func (q *queueWithMetrics) Close() error {
	q.messageReceiptTime.Stop()
	return nil
}

func (q *queueWithMetrics) Push(msg *DecodedSSVMessage) {
	q.Queue.TryPush(msg)

	msgID := msg.GetID()
	q.processPushedMessage(msgID)
}

func (q *queueWithMetrics) TryPush(msg *DecodedSSVMessage) bool {
	msgID := msg.GetID()

	pushed := q.Queue.TryPush(msg)
	if !pushed {
		q.metrics.DroppedQueueMessage(msgID)
	}

	q.processPushedMessage(msgID)

	return pushed
}

func (q *queueWithMetrics) Pop(ctx context.Context, mp MessagePrioritizer, f Filter) *DecodedSSVMessage {
	msg := q.Queue.Pop(ctx, mp, f)

	q.processPoppedMessage(msg.GetID())

	return msg
}

func (q *queueWithMetrics) TryPop(mp MessagePrioritizer, f Filter) *DecodedSSVMessage {
	msg := q.Queue.TryPop(mp, f)

	q.processPoppedMessage(msg.GetID())

	return msg
}

func (q *queueWithMetrics) processPushedMessage(msgID spectypes.MessageID) {
	q.metrics.IncomingQueueMessage(msgID)
	q.messageReceiptTime.Set(msgID, time.Now(), defaultTTL)
}

func (q *queueWithMetrics) processPoppedMessage(msgID spectypes.MessageID) {
	q.metrics.OutgoingQueueMessage(msgID)

	timeInQueue := defaultTTL
	entry := q.messageReceiptTime.Get(msgID)
	if entry != nil {
		timeInQueue = time.Since(entry.Value())
	}
	q.metrics.MessageTimeInQueue(msgID, timeInQueue)
}

func (q *queueWithMetrics) Len() int {
	l := q.Queue.Len()
	c := q.Queue.Cap()

	q.metrics.MessageQueueSize(l)
	q.metrics.MessageQueueCapacity(c)

	return l
}
