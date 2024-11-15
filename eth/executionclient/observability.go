package executionclient

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
)

const (
	observabilityComponentName      = "github.com/ssvlabs/ssv/eth/executionclient"
	observabilityComponentNamespace = "ssv.el"
)

type executionClientStatus string

const (
	statusSyncing executionClientStatus = "syncing"
	statusFailure executionClientStatus = "failure"
	statusReady   executionClientStatus = "ready"
)

var (
	meter            = otel.Meter(observabilityComponentName)
	latencyHistogram = observability.GetMetric(
		fmt.Sprintf("%s.latency.duration", observabilityComponentNamespace),
		func(metricName string) (metric.Float64Histogram, error) {
			return meter.Float64Histogram(
				metricName,
				metric.WithUnit("s"),
				metric.WithDescription("execution client latency in seconds"),
				metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...),
			)
		},
	)

	syncingDistanceGauge = observability.GetMetric(
		fmt.Sprintf("%s.syncing.distance", observabilityComponentNamespace),
		func(metricName string) (metric.Int64Gauge, error) {
			return meter.Int64Gauge(
				metricName,
				metric.WithUnit("{block}"),
				metric.WithDescription("execution client syncing distance which is a delta between highest and current blocks"))
		},
	)

	clientStatusGauge = observability.GetMetric(
		fmt.Sprintf("%s.syncing.status", observabilityComponentNamespace),
		func(metricName string) (metric.Int64Gauge, error) {
			return meter.Int64Gauge(
				metricName,
				metric.WithDescription("execution client syncing status"))
		},
	)

	lastProcessedBlockGauge = observability.GetMetric(
		fmt.Sprintf("%s.syncing.last_processed_block", observabilityComponentNamespace),
		func(metricName string) (metric.Int64Gauge, error) {
			return meter.Int64Gauge(
				metricName,
				metric.WithUnit("{block_number}"),
				metric.WithDescription("last processed block by execution client"))
		},
	)
)

func executionClientAddrAttribute(value string) attribute.KeyValue {
	eventNameAttrName := fmt.Sprintf("%s.addr", observabilityComponentNamespace)
	return attribute.String(eventNameAttrName, value)
}

func executionClientStatusAttribute(value executionClientStatus) attribute.KeyValue {
	eventNameAttrName := fmt.Sprintf("%s.status", observabilityComponentNamespace)
	return attribute.String(eventNameAttrName, string(value))
}

func recordExecutionClientStatus(ctx context.Context, status executionClientStatus, nodeAddr string) {
	clientStatusGauge.Record(ctx, 0,
		metric.WithAttributes(executionClientAddrAttribute(nodeAddr)),
		metric.WithAttributes(executionClientStatusAttribute(statusReady)),
	)
	clientStatusGauge.Record(ctx, 0,
		metric.WithAttributes(executionClientAddrAttribute(nodeAddr)),
		metric.WithAttributes(executionClientStatusAttribute(statusSyncing)),
	)
	clientStatusGauge.Record(ctx, 0,
		metric.WithAttributes(executionClientAddrAttribute(nodeAddr)),
		metric.WithAttributes(executionClientStatusAttribute(statusFailure)),
	)

	clientStatusGauge.Record(ctx, 1,
		metric.WithAttributes(executionClientAddrAttribute(nodeAddr)),
		metric.WithAttributes(executionClientStatusAttribute(status)),
	)
}
