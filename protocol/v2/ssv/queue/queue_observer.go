package queue

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// queueObserver is the queue's hook for inbox-size and drop telemetry.
// It lets queue.go stay focused on queueing semantics while metrics live behind
// a small interface that can be swapped out (for tests or queues that don't
// publish metrics).
type queueObserver interface {
	recordInboxSize(int64)
	recordDrop(string)
}

type noopQueueObserver struct{}

func (noopQueueObserver) recordInboxSize(int64) {}

func (noopQueueObserver) recordDrop(string) {}

type metricsQueueObserver struct {
	inboxSizeMetric    metric.Int64Gauge
	inboxSizeRecordOps []metric.RecordOption
	queueType          string
	queueID            string
}

// WithInboxSizeMetric configures queue-level observability for inbox size and dropped messages.
func WithInboxSizeMetric(inboxSizeMetric metric.Int64Gauge, queueType, queueID string) Option {
	attrSet := attribute.NewSet(
		attribute.String("ssv.queue.type", queueType),
		attribute.String("ssv.queue.id", queueID),
	)

	return func(q *priorityQueue) {
		q.observer = metricsQueueObserver{
			inboxSizeMetric:    inboxSizeMetric,
			inboxSizeRecordOps: []metric.RecordOption{metric.WithAttributeSet(attrSet)},
			queueType:          queueType,
			queueID:            queueID,
		}
	}
}

func (o metricsQueueObserver) recordInboxSize(inboxSize int64) {
	if o.inboxSizeMetric == nil {
		return
	}
	o.inboxSizeMetric.Record(
		context.Background(),
		inboxSize,
		o.inboxSizeRecordOps...,
	)
}

func (o metricsQueueObserver) recordDrop(reason string) {
	recordDroppedMessage(o.queueType, o.queueID, reason)
}
