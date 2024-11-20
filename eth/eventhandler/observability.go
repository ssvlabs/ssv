package eventhandler

import (
	"context"
	"fmt"

	"github.com/ssvlabs/ssv/observability"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	observabilityComponentName      = "github.com/ssvlabs/ssv/eth/eventhandler"
	observabilityComponentNamespace = "ssv.event_syncer.handler"
)

var (
	meter = otel.Meter(observabilityComponentName)

	eventsProcessSuccessCounter = observability.NewMetric(
		fmt.Sprintf("%s.events_processed", observabilityComponentNamespace),
		func(metricName string) (metric.Int64Counter, error) {
			return meter.Int64Counter(
				metricName,
				metric.WithUnit("{event}"),
				metric.WithDescription("total number of successfully processed events(logs)"))
		},
	)

	eventsProcessFailureCounter = observability.NewMetric(
		fmt.Sprintf("%s.events_failed", observabilityComponentNamespace),
		func(metricName string) (metric.Int64Counter, error) {
			return meter.Int64Counter(
				metricName,
				metric.WithUnit("{event}"),
				metric.WithDescription("total number of failures during event(log) processing"))
		},
	)

	lastProcessedBlockGauge = observability.NewMetric(
		fmt.Sprintf("%s.last_processed_block", observabilityComponentNamespace),
		func(metricName string) (metric.Int64Gauge, error) {
			return meter.Int64Gauge(
				metricName,
				metric.WithUnit("{block_number}"),
				metric.WithDescription("last processed block by event handler"))
		},
	)
)

func eventNameAttribute(eventName string) attribute.KeyValue {
	const eventNameAttrName = "ssv.event_syncer.event_name"
	return attribute.String(eventNameAttrName, eventName)
}

func recordEventProcessFailure(ctx context.Context, eventName string) {
	eventsProcessFailureCounter.Add(ctx, 1, metric.WithAttributes(eventNameAttribute(eventName)))
}
