package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/ssvlabs/ssv/observability"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/ibft/storage"
	observabilityNamespace = "ssv.storage"
)

var (
	meter = otel.Meter(observabilityName)

	operationDurationHistogram = observability.NewMetric(
		meter.Float64Histogram(
			metricName("operation.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("participants db ops duration"),
			metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...)))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

func recordSaveDuration(name string, from time.Time) {
	duration := time.Since(from)
	operationDurationHistogram.Record(
		context.Background(),
		duration.Seconds(),
		metric.WithAttributes(
			semconv.DBCollectionName(name),
			semconv.DBOperationName("save"),
		),
	)
}
