package queue

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

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

// WithQueueMetrics configures queue-level observability for inbox size and dropped messages.
func WithQueueMetrics(inboxSizeMetric metric.Int64Gauge, queueType string, queueID string) Option {
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
	if o.queueType == "" || o.queueID == "" {
		return
	}
	RecordDroppedMessage(o.queueType, o.queueID, reason)
}
