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
// meaningful, records a slot-delay histogram point. For roles where
// duty.Slot is not the intended firing slot (see dutySlotIsFiringSlot), the
// caller's slotDelay does not reflect operator lateness, so the histogram
// point is skipped; the counter still ticks.
func recordDutyScheduled(ctx context.Context, role types.RunnerRole, slotDelay time.Duration) {
	runnerRoleAttr := metric.WithAttributes(observability.RunnerRoleAttribute(role))
	dutiesScheduledCounter.Add(ctx, 1, runnerRoleAttr)
	if dutySlotIsFiringSlot(role) {
		slotDelayHistogram.Record(ctx, slotDelay.Seconds(), runnerRoleAttr)
	}
}

// dutySlotIsFiringSlot reports whether duty.Slot for the given role
// represents the wall-clock slot at which this operator intends to fire its
// duty. True for most roles (attester, proposer, etc.); false for roles where
// duty.Slot is a shared coordination point intentionally held in the past
// (the operator fires later than duty.Slot) — see
// voluntaryExitDutySlotsToPostpone for the canonical rationale.
func dutySlotIsFiringSlot(role types.RunnerRole) bool {
	return role != types.RoleVoluntaryExit
}
