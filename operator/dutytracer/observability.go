package validator

import (
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/operator/dutytracer"
	observabilityNamespace = "ssv.dutytracer"
)

var (
	meter = otel.Meter(observabilityName)

	tracerInFlightMessageCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("messages.received"),
			metric.WithDescription("total number of messages received tracer intercepted")))

	tracerInFlightMessageHist = observability.NewMetric(
		meter.Float64Histogram(
			metricName("messages.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("message processing duration"),
			metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...)))

	tracerDBDurationHistogram = observability.NewMetric(
		meter.Float64Histogram(
			metricName("db.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("db interaction duration"),
			metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...)))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}
