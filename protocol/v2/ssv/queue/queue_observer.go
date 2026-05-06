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

	dropAddOpsByReason := map[string][]metric.AddOption{
		DropReasonBufferFull: {
			metric.WithAttributeSet(attribute.NewSet(
				attribute.String("ssv.queue.type", queueType),
				attribute.String("ssv.queue.id", queueID),
				attribute.String("ssv.queue.drop_reason", DropReasonBufferFull),
			)),
		},
	}

	return func(q *priorityQueue) {
		q.observer = metricsQueueObserver{
			inboxSizeMetric:    inboxSizeMetric,
			inboxSizeRecordOps: []metric.RecordOption{metric.WithAttributeSet(queueAttrSet)},
			queueType:          queueType,
			queueID:            queueID,
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
	if ops, ok := o.dropAddOpsByReason[reason]; ok {
		droppedMessagesMetric.Add(context.Background(), 1, ops...)
		return
	}
	// Unregistered reason — fall back to per-call allocation so the metric
	// still records (no silent miss). Adding a new reason should also extend
	// the pre-built map above to keep the hot path zero-alloc.
	droppedMessagesMetric.Add(
		context.Background(),
		1,
		metric.WithAttributes(
			attribute.String("ssv.queue.type", o.queueType),
			attribute.String("ssv.queue.id", o.queueID),
			attribute.String("ssv.queue.drop_reason", reason),
		),
	)
}
