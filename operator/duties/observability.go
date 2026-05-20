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

// recordDutyScheduled bumps the per-role scheduled-duty counter and, when
// meaningful, records a slot-delay histogram point. For wire-slot roles
// (see usesWireSlot), the caller's slotDelay is intentionally large and
// does not reflect operator lateness, so the histogram point is skipped;
// the counter still ticks.
func recordDutyScheduled(ctx context.Context, role types.RunnerRole, slotDelay time.Duration) {
	runnerRoleAttr := metric.WithAttributes(observability.RunnerRoleAttribute(role))
	dutiesScheduledCounter.Add(ctx, 1, runnerRoleAttr)
	if !usesWireSlot(role) {
		slotDelayHistogram.Record(ctx, slotDelay.Seconds(), runnerRoleAttr)
	}
}

// usesWireSlot reports whether duty.Slot for the given role is a wire /
// coordination slot intentionally kept in the past (rather than the
// wall-clock firing slot). For such roles, "lateness" relative to duty.Slot
// is meaningless — see voluntaryExitWireSlotsToPostpone for the canonical
// rationale.
func usesWireSlot(role types.RunnerRole) bool {
	return role == types.RoleVoluntaryExit
}
