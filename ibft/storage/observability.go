package storage

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/ibft/storage"
	observabilityNamespace = "ssv.storage"
)

var (
	meter = otel.Meter(observabilityName)

	operationDurationHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "operation.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("participants db ops duration"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))
)

func recordSaveDuration(name string, duration time.Duration) {
	operationDurationHistogram.Record(
		context.Background(),
		duration.Seconds(),
		metric.WithAttributes(
			semconv.DBCollectionName(name),
			semconv.DBOperationName("save"),
		),
	)
}
