package queue

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
)

// Metrics records metrics about the Queue.
type Metrics interface {
	DroppedQueueMessage(messageID spectypes.MessageID)
}

type queueWithMetrics struct {
	Queue
	metrics Metrics
}

// WithMetrics returns a wrapping of the given Queue that records metrics using the given Metrics.
func WithMetrics(q Queue, metrics Metrics) Queue {
	return &queueWithMetrics{
		Queue:   q,
		metrics: metrics,
	}
}

func (q *queueWithMetrics) TryPush(msg *DecodedSSVMessage) bool {
	pushed := q.Queue.TryPush(msg)
	if !pushed {
		q.metrics.DroppedQueueMessage(msg.GetID())
	}

	return pushed
}
