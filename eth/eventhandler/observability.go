package eventhandler

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/eth/eventhandler"
	observabilityNamespace = "ssv.event_syncer.handler"
)

var (
	meter = otel.Meter(observabilityName)

	eventsProcessSuccessCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("events_processed"),
			metric.WithUnit("{event}"),
			metric.WithDescription("total number of successfully processed events(logs)")))

	eventsProcessFailureCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("events_failed"),
			metric.WithUnit("{event}"),
			metric.WithDescription("total number of failures during event(log) processing")))

	lastProcessedBlockGauge = observability.NewMetric(
		meter.Int64Gauge(
			metricName("last_processed_block"),
			metric.WithUnit("{block_number}"),
			metric.WithDescription("last processed block by event handler")))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

func eventNameAttribute(eventName string) attribute.KeyValue {
	const eventNameAttrName = "ssv.event_syncer.event_name"
	return attribute.String(eventNameAttrName, eventName)
}

func recordEventProcessFailure(ctx context.Context, eventName string) {
	eventsProcessFailureCounter.Add(ctx, 1, metric.WithAttributes(eventNameAttribute(eventName)))
}
