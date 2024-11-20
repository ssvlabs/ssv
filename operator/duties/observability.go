package duties

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
)

const (
	observabilityComponentName      = "github.com/ssvlabs/ssv/operator/duties"
	observabilityComponentNamespace = "ssv.operator.duty_scheduler"
)

var (
	meter = otel.Meter(observabilityComponentName)

	slotDelayHistogram = observability.NewMetric(
		fmt.Sprintf("%s.slot_ticker_delay.duration", observabilityComponentNamespace),
		func(metricName string) (metric.Float64Histogram, error) {
			return meter.Float64Histogram(
				metricName,
				metric.WithUnit("s"),
				metric.WithDescription("delay of the slot ticker in seconds"),
				metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...))
		},
	)

	dutiesExecutedCounter = observability.NewMetric(
		fmt.Sprintf("%s.executions", observabilityComponentNamespace),
		func(metricName string) (metric.Int64Counter, error) {
			return meter.Int64Counter(
				metricName,
				metric.WithUnit("{duty}"),
				metric.WithDescription("total number of duties executed by scheduler"))
		},
	)

	committeeDutiesExecutedCounter = observability.NewMetric(
		fmt.Sprintf("%s.committee_executions", observabilityComponentNamespace),
		func(metricName string) (metric.Int64Counter, error) {
			return meter.Int64Counter(
				metricName,
				metric.WithUnit("{committee_duty}"),
				metric.WithDescription("total number of committee duties executed by scheduler"))
		},
	)
)

func recordDutyExecuted[T observability.BeaconRole](ctx context.Context, beaconRole T) {
	dutiesExecutedCounter.Add(ctx, 1, metric.WithAttributes(observability.BeaconRoleAttribute(beaconRole)))
}
