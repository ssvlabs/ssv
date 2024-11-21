package duties

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/operator/duties"
	observabilityNamespace = "ssv.duty.scheduler"
)

var (
	meter = otel.Meter(observabilityName)

	slotDelayHistogram = observability.NewMetric(
		meter.Float64Histogram(
			metricName("slot_ticker_delay.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("delay of the slot ticker in seconds"),
			metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...)))

	dutiesExecutedCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("executions"),
			metric.WithUnit("{duty}"),
			metric.WithDescription("total number of duties executed by scheduler")))

	committeeDutiesExecutedCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("committee_executions"),
			metric.WithUnit("{committee_duty}"),
			metric.WithDescription("total number of committee duties executed by scheduler")))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

func recordDutyExecuted[T observability.BeaconRole](ctx context.Context, beaconRole T) {
	dutiesExecutedCounter.Add(ctx, 1, metric.WithAttributes(observability.BeaconRoleAttribute(beaconRole)))
}
