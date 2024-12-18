package executionclient

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

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

	requestDurationHistogram = observability.NewMetric(
		meter.Float64Histogram(
			metricName("request.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("execution client request duration in seconds"),
			metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...)))

	syncDistanceGauge = observability.NewMetric(
		meter.Int64Gauge(
			metricName("sync.distance"),
			metric.WithUnit("{block}"),
			metric.WithDescription("execution client sync distance which is a delta between highest and current blocks")))

	clientStatusGauge = observability.NewMetric(
		meter.Int64Gauge(
			metricName("sync.status"),
			metric.WithDescription("execution client sync status")))

	lastProcessedBlockGauge = observability.NewMetric(
		meter.Int64Gauge(
			metricName("sync.last_processed_block"),
			metric.WithUnit("{block_number}"),
			metric.WithDescription("last processed block by execution client")))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

func recordRequestDuration(ctx context.Context, serverAddr string, duration time.Duration) {
	requestDurationHistogram.Record(
		ctx,
		duration.Seconds(),
		metric.WithAttributes(semconv.ServerAddress(serverAddr)))
}

func executionClientStatusAttribute(value executionClientStatus) attribute.KeyValue {
	eventNameAttrName := fmt.Sprintf("%s.status", observabilityNamespace)
	return attribute.String(eventNameAttrName, string(value))
}

func recordExecutionClientStatus(ctx context.Context, status executionClientStatus, nodeAddr string) {
	resetExecutionClientStatusGauge(ctx, nodeAddr)

	clientStatusGauge.Record(ctx, 1,
		metric.WithAttributes(semconv.ServerAddress(nodeAddr)),
		metric.WithAttributes(executionClientStatusAttribute(status)),
	)
}

func resetExecutionClientStatusGauge(ctx context.Context, nodeAddr string) {
	for _, status := range []executionClientStatus{statusReady, statusSyncing, statusFailure} {
		clientStatusGauge.Record(ctx, 0,
			metric.WithAttributes(semconv.ServerAddress(nodeAddr)),
			metric.WithAttributes(executionClientStatusAttribute(status)),
		)
	}
}
