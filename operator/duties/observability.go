package duties

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/observability"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/operator/duties"
	observabilityNamespace = "ssv.duty"
)

var (
	tracer = otel.Tracer(observabilityName)
	meter  = otel.Meter(observabilityName)

	slotDelayHistogram = observability.NewMetric(
		meter.Float64Histogram(
			metricName("scheduler.slot_ticker_delay.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("delay of the slot ticker in seconds"),
			metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...)))

	dutiesExecutedCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("scheduler.executions"),
			metric.WithUnit("{duty}"),
			metric.WithDescription("total number of duties executed by scheduler")))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

func recordDutyExecuted(ctx context.Context, role types.RunnerRole) {
	dutiesExecutedCounter.Add(ctx, 1,
		metric.WithAttributes(
			observability.RunnerRoleAttribute(role),
		))
}
