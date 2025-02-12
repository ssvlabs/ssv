package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/ssvlabs/ssv/observability"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/iqbft/storage"
	observabilityNamespace = "ssv.storage"
)

var (
	meter = otel.Meter(observabilityName)

	saveParticipantsDurationHistogram = observability.NewMetric(
		meter.Float64Histogram(
			metricName("save.duration"),
			metric.WithUnit("ms"),
			metric.WithDescription("participants save duration"),
			metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...)))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

type metrics struct {
	stageStart time.Time
	name       string
}

func newMetrics(role string) *metrics {
	return &metrics{
		name: role,
	}
}

func (m *metrics) Start() {
	m.stageStart = time.Now()
}

func (m *metrics) End() {
	saveParticipantsDurationHistogram.Record(
		context.Background(),
		float64(time.Since(m.stageStart).Milliseconds()),
		metric.WithAttributes(attribute.String("store", m.name)),
	)
	m.stageStart = time.Now()
}
