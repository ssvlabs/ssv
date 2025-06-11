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

	// MultiClient metrics
	clientSwitchCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("client.switch"),
			metric.WithDescription("number of times the execution client has been switched")))

	multiClientMethodCallsCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("multi_client.method_calls"),
			metric.WithDescription("number of method calls to the multi client")))

	multiClientMethodErrorsCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("multi_client.method_errors"),
			metric.WithDescription("number of method call errors in the multi client")))

	multiClientMethodDurationHistogram = observability.NewMetric(
		meter.Float64Histogram(
			metricName("multi_client.method_duration"),
			metric.WithUnit("s"),
			metric.WithDescription("multi client method call duration in seconds"),
			metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...)))

	healthyClientsGauge = observability.NewMetric(
		meter.Int64Gauge(
			metricName("multi_client.healthy_clients"),
			metric.WithUnit("{clients}"),
			metric.WithDescription("number of healthy clients in the multi client")))

	allClientsGauge = observability.NewMetric(
		meter.Int64Gauge(
			metricName("multi_client.all_clients"),
			metric.WithUnit("{clients}"),
			metric.WithDescription("number of clients in the multi client")))

	clientInitCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("client.init"),
			metric.WithDescription("number of times a client was initialized")))
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

// recordClientSwitch records a client switch event
func recordClientSwitch(ctx context.Context, fromAddr, toAddr string) {
	clientSwitchCounter.Add(ctx, 1,
		metric.WithAttributes(
			executionClientFromAddrAttribute(fromAddr),
			executionClientToAddrAttribute(toAddr),
		),
	)
}

func executionClientFromAddrAttribute(value string) attribute.KeyValue {
	eventNameAttrName := fmt.Sprintf("%s.from_addr", observabilityNamespace)
	return attribute.String(eventNameAttrName, value)
}

func executionClientToAddrAttribute(value string) attribute.KeyValue {
	eventNameAttrName := fmt.Sprintf("%s.to_addr", observabilityNamespace)
	return attribute.String(eventNameAttrName, value)
}

// recordMultiClientMethodCall records metrics for multi client method calls
func recordMultiClientMethodCall(ctx context.Context, method string, nodeAddr string, duration time.Duration, err error) {
	attrs := []attribute.KeyValue{
		executionMultiClientMethodAttribute(method),
		semconv.ServerAddress(nodeAddr),
	}

	multiClientMethodCallsCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	multiClientMethodDurationHistogram.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))

	if err != nil {
		multiClientMethodErrorsCounter.Add(ctx, 1)
	}
}

func executionMultiClientMethodAttribute(value string) attribute.KeyValue {
	eventNameAttrName := fmt.Sprintf("%s.method", observabilityNamespace)
	return attribute.String(eventNameAttrName, value)
}

// recordHealthyClientsCount records the number of healthy clients
func recordHealthyClientsCount(ctx context.Context, count int64) {
	healthyClientsGauge.Record(ctx, count)
}

// recordAllClientsCount records the number of clients
func recordAllClientsCount(ctx context.Context, count int64) {
	allClientsGauge.Record(ctx, count)
}

// recordClientInitStatus records metrics for client initialization
func recordClientInitStatus(ctx context.Context, nodeAddr string, success bool) {
	attrs := []attribute.KeyValue{
		semconv.ServerAddress(nodeAddr),
		executionClientInitStatusAttribute(success),
	}

	clientInitCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
}

func executionClientInitStatusAttribute(value bool) attribute.KeyValue {
	eventNameAttrName := fmt.Sprintf("%s.init.status", observabilityNamespace)
	return attribute.Bool(eventNameAttrName, value)
}
