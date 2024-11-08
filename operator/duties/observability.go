package duties

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

const observabilityComponentName = "github.com/ssvlabs/ssv/operator/duties"

var (
	meter = otel.Meter(observabilityComponentName)

	slotDelayHistogram    metric.Float64Histogram
	dutiesExecutedCounter metric.Int64Counter
)

func init() {
	var err error

	logger := zap.L().With(zap.String("component", observabilityComponentName))

	slotDelayHistogram, err = meter.Float64Histogram(
		"scheduler.slot_ticker.delay.duration",
		metric.WithUnit("s"),
		metric.WithDescription("The delay of the slot ticker"),
		metric.WithExplicitBucketBoundaries([]float64{5, 10, 20, 100, 500, 5000}...))
	if err != nil {
		logger.Error("failed to instantiate metric",
			zap.String("metric_name", "scheduler.slot_ticker.delay.duration"),
			zap.Error(err))
	}

	dutiesExecutedCounter, err = meter.Int64Counter(
		"scheduler.duty.execution.count",
		metric.WithUnit("{duty}"),
		metric.WithDescription("Total number of duties executed by scheduler"))
	if err != nil {
		logger.Error("failed to instantiate metric",
			zap.String("metric_name", "scheduler.duty.execution.count"),
			zap.Error(err))
	}
}
