package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/ssvlabs/ssv/observability"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/iqbft/storage"
	observabilityNamespace = "ssv.storage"
)

var (
	meter = otel.Meter(observabilityName)

	saveParticipantsDurationHistogram = observability.NewMetric(
		meter.Float64Histogram(
			metricName("operation.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("participants save duration"),
			metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...)))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

func recordRequestDuration(name string, from time.Time, updated bool) {
	duration := time.Since(from)
	saveParticipantsDurationHistogram.Record(
		context.Background(),
		duration.Seconds(),
		metric.WithAttributes(
			semconv.DBOperationName(name),
			attribute.Bool("db.operation.is_update", updated),
		),
	)
}
