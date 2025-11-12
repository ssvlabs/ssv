package duties

import (
	"context"
	"time"

	"github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/operator/duties"
	observabilityNamespace = "ssv.duty"
)

var (
	tracer = otel.Tracer(observabilityName)
	meter  = otel.Meter(observabilityName)

	slotDelayHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "scheduler.slot_ticker_delay.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("delay of the slot ticker in seconds"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	dutiesScheduledCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "scheduler.executions"),
			metric.WithUnit("{duty}"),
			metric.WithDescription("total number of duties scheduled for execution")))
)

func recordDutyScheduled(ctx context.Context, role types.RunnerRole, slotDelay time.Duration) {
	runnerRoleAttr := metric.WithAttributes(observability.RunnerRoleAttribute(role))
	dutiesScheduledCounter.Add(ctx, 1, runnerRoleAttr)
	slotDelayHistogram.Record(ctx, slotDelay.Seconds(), runnerRoleAttr)
}
