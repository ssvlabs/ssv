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
	observabilityName      = "github.com/ssvlabs/ssv/eth/executionclient"
	observabilityNamespace = "ssv.el"
)

type executionClientStatus string

const (
	statusSyncing executionClientStatus = "syncing"
	statusFailure executionClientStatus = "failure"
	statusReady   executionClientStatus = "ready"
)

var (
	meter = otel.Meter(observabilityName)

	latencyHistogram = observability.NewMetric(
		meter.Float64Histogram(
			metricName("latency.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("execution client latency in seconds"),
			metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...)))

	syncingDistanceGauge = observability.NewMetric(
		meter.Int64Gauge(
			metricName("syncing.distance"),
			metric.WithUnit("{block}"),
			metric.WithDescription("execution client syncing distance which is a delta between highest and current blocks")))

	clientStatusGauge = observability.NewMetric(
		meter.Int64Gauge(
			metricName("syncing.status"),
			metric.WithDescription("execution client syncing status")))

	lastProcessedBlockGauge = observability.NewMetric(
		meter.Int64Gauge(
			metricName("syncing.last_processed_block"),
			metric.WithUnit("{block_number}"),
			metric.WithDescription("last processed block by execution client")))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

func executionClientAddrAttribute(value string) attribute.KeyValue {
	eventNameAttrName := fmt.Sprintf("%s.addr", observabilityNamespace)
	return attribute.String(eventNameAttrName, value)
}

func executionClientStatusAttribute(value executionClientStatus) attribute.KeyValue {
	eventNameAttrName := fmt.Sprintf("%s.status", observabilityNamespace)
	return attribute.String(eventNameAttrName, string(value))
}

func recordExecutionClientStatus(ctx context.Context, status executionClientStatus, nodeAddr string) {
	resetExecutionClientStatusGauge(ctx, nodeAddr)

	clientStatusGauge.Record(ctx, 1,
		metric.WithAttributes(executionClientAddrAttribute(nodeAddr)),
		metric.WithAttributes(executionClientStatusAttribute(status)),
	)
}

func resetExecutionClientStatusGauge(ctx context.Context, nodeAddr string) {
	for _, status := range []executionClientStatus{statusReady, statusSyncing, statusFailure} {
		clientStatusGauge.Record(ctx, 0,
			metric.WithAttributes(executionClientAddrAttribute(nodeAddr)),
			metric.WithAttributes(executionClientStatusAttribute(status)),
		)
	}
}
