package queue

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Attribute keys for the queue's telemetry.
const (
	attrKeyQueueType   = "ssv.queue.type"
	attrKeyQueueID     = "ssv.queue.id"
	attrKeyDropReason  = "ssv.queue.drop_reason"
	attrKeyPurgeReason = "ssv.queue.purge_reason"
)

// queueObserver is the queue's hook for inbox-size, drop and purge telemetry. It keeps queue.go focused
// on queueing semantics while metrics live behind a small interface that can be swapped out (for tests,
// or queues that don't publish metrics).
type queueObserver interface {
	recordInboxSize(int64)
	recordDrop(string)
	recordPurge(string)
}

type noopQueueObserver struct{}

func (noopQueueObserver) recordInboxSize(int64) {}
func (noopQueueObserver) recordDrop(string)     {}
func (noopQueueObserver) recordPurge(string)    {}

type metricsQueueObserver struct {
	inboxSizeMetric    metric.Int64Gauge
	inboxSizeRecordOps []metric.RecordOption
	queueType          string
	queueID            string
	// dropAddOpsByReason and purgeAddOpsByReason hold pre-built AddOptions per known reason so the hot
	// path allocates no attributes per dropped/purged message.
	dropAddOpsByReason  map[string][]metric.AddOption
	purgeAddOpsByReason map[string][]metric.AddOption
}

// WithQueueMetrics configures queue-level observability for inbox size, drops and purges.
func WithQueueMetrics(inboxSizeMetric metric.Int64Gauge, queueType, queueID string) Option {
	queueAttrSet := attribute.NewSet(
		attribute.String(attrKeyQueueType, queueType),
		attribute.String(attrKeyQueueID, queueID),
	)

	return func(q *priorityQueue) {
		q.observer = metricsQueueObserver{
			inboxSizeMetric:     inboxSizeMetric,
			inboxSizeRecordOps:  []metric.RecordOption{metric.WithAttributeSet(queueAttrSet)},
			queueType:           queueType,
			queueID:             queueID,
			dropAddOpsByReason:  reasonAddOps(queueType, queueID, attrKeyDropReason, DropReasonBufferFull),
			purgeAddOpsByReason: reasonAddOps(queueType, queueID, attrKeyPurgeReason, PurgeReasonStale),
		}
	}
}

// reasonAddOps pre-builds the AddOption slice for each reason so recording allocates no attributes.
func reasonAddOps(queueType, queueID, reasonKey string, reasons ...string) map[string][]metric.AddOption {
	byReason := make(map[string][]metric.AddOption, len(reasons))
	for _, reason := range reasons {
		byReason[reason] = []metric.AddOption{
			metric.WithAttributeSet(attribute.NewSet(
				attribute.String(attrKeyQueueType, queueType),
				attribute.String(attrKeyQueueID, queueID),
				attribute.String(reasonKey, reason),
			)),
		}
	}
	return byReason
}

func (o metricsQueueObserver) recordInboxSize(inboxSize int64) {
	if o.inboxSizeMetric == nil {
		return
	}
	o.inboxSizeMetric.Record(context.Background(), inboxSize, o.inboxSizeRecordOps...)
}

func (o metricsQueueObserver) recordDrop(reason string) {
	o.record(droppedMessagesMetric, o.dropAddOpsByReason, attrKeyDropReason, reason)
}

func (o metricsQueueObserver) recordPurge(reason string) {
	o.record(purgedMessagesMetric, o.purgeAddOpsByReason, attrKeyPurgeReason, reason)
}

// record adds 1 to counter for reason, using the pre-built AddOptions when the reason is known and
// allocating per call otherwise, so an unregistered reason still records. Extend the pre-built map when
// adding a reason to keep the hot path allocation-free.
func (o metricsQueueObserver) record(counter metric.Int64Counter, opsByReason map[string][]metric.AddOption, reasonKey, reason string) {
	if ops, ok := opsByReason[reason]; ok {
		counter.Add(context.Background(), 1, ops...)
		return
	}
	counter.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String(attrKeyQueueType, o.queueType),
		attribute.String(attrKeyQueueID, o.queueID),
		attribute.String(reasonKey, reason),
	))
}
