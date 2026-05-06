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
	// dropAddOpsByReason holds pre-built AddOptions per known drop reason so
	// the hot path doesn't allocate attributes on every dropped message.
	dropAddOpsByReason map[string][]metric.AddOption
}

// WithQueueMetrics configures queue-level observability for inbox size and dropped messages.
func WithQueueMetrics(inboxSizeMetric metric.Int64Gauge, queueType, queueID string) Option {
	queueAttrSet := attribute.NewSet(
		attribute.String("ssv.queue.type", queueType),
		attribute.String("ssv.queue.id", queueID),
	)

	dropAddOpsByReason := make(map[string][]metric.AddOption, 1)
	for _, reason := range []string{DropReasonBufferFull} {
		dropAddOpsByReason[reason] = []metric.AddOption{
			metric.WithAttributeSet(attribute.NewSet(
				attribute.String("ssv.queue.type", queueType),
				attribute.String("ssv.queue.id", queueID),
				attribute.String("ssv.queue.drop_reason", reason),
			)),
		}
	}

	return func(q *priorityQueue) {
		q.observer = metricsQueueObserver{
			inboxSizeMetric:    inboxSizeMetric,
			inboxSizeRecordOps: []metric.RecordOption{metric.WithAttributeSet(queueAttrSet)},
			dropAddOpsByReason: dropAddOpsByReason,
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
	ops, ok := o.dropAddOpsByReason[reason]
	if !ok {
		return
	}
	droppedMessagesMetric.Add(context.Background(), 1, ops...)
}
