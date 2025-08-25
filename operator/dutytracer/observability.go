package validator

import (
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability/metrics"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/operator/dutytracer"
	observabilityNamespace = "ssv.dutytracer"
)

var (
	meter = otel.Meter(observabilityName)

	tracerInFlightMessageCounter = metrics.New(
		meter.Int64Counter(
			metricName("messages.received"),
			metric.WithDescription("total number of messages received tracer intercepted")))

	tracerInFlightMessageHist = metrics.New(
		meter.Float64Histogram(
			metricName("messages.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("message processing duration"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	tracerDBDurationHistogram = metrics.New(
		meter.Float64Histogram(
			metricName("db.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("db interaction duration"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}
