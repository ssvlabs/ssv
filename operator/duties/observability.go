package duties

import (
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

const (
	observabilityComponentName      = "github.com/ssvlabs/ssv/operator/duties"
	observabilityComponentNamespace = "ssv.operator.duty"
)

var (
	meter = otel.Meter(observabilityComponentName)

	slotDelayHistogram    metric.Float64Histogram
	dutiesExecutedCounter metric.Int64Counter
)

func init() {
	var err error

	logger := zap.L().With(zap.String("component", observabilityComponentName))

	slotDelayMetricName := fmt.Sprintf("%s.scheduler.slot_ticker.delay.duration", observabilityComponentNamespace)
	slotDelayHistogram, err = meter.Float64Histogram(
		slotDelayMetricName,
		metric.WithUnit("s"),
		metric.WithDescription("The delay of the slot ticker"),
		metric.WithExplicitBucketBoundaries([]float64{5, 10, 20, 100, 500, 5000}...))
	if err != nil {
		logger.Error("failed to instantiate metric",
			zap.String("metric_name", slotDelayMetricName),
			zap.Error(err))
	}

	dutyExecutedCounterMetricName := fmt.Sprintf("%s.scheduler.executions", observabilityComponentNamespace)
	dutiesExecutedCounter, err = meter.Int64Counter(
		dutyExecutedCounterMetricName,
		metric.WithUnit("{duty}"),
		metric.WithDescription("Total number of duties executed by scheduler"))
	if err != nil {
		logger.Error("failed to instantiate metric",
			zap.String("metric_name", dutyExecutedCounterMetricName),
			zap.Error(err))
	}
}
