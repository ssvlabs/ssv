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
// duty.Slot is not the intended execution slot (see dutySlotIsExecutionSlot),
// the caller's slotDelay does not reflect operator lateness, so the histogram
// point is skipped; the counter still ticks.
func recordDutyScheduled(ctx context.Context, role types.RunnerRole, slotDelay time.Duration) {
	runnerRoleAttr := metric.WithAttributes(observability.RunnerRoleAttribute(role))
	dutiesScheduledCounter.Add(ctx, 1, runnerRoleAttr)
	if dutySlotIsExecutionSlot(role) {
		slotDelayHistogram.Record(ctx, slotDelay.Seconds(), runnerRoleAttr)
	}
}

// dutySlotIsExecutionSlot reports whether duty.Slot for the given role
// represents the wall-clock slot at which this operator intends to execute
// its duty. True for most roles (attester, proposer, etc.); false for roles
// where duty.Slot is a shared coordination point that does not coincide with
// execution, so measuring slotDelay against it is meaningless and the lateness
// check is skipped. This happens in either direction:
//   - voluntary-exit and validator-registration hold duty.Slot in the past and
//     execute later (see voluntaryExitDutySlotsToPostpone and
//     validatorRegistrationDutySlotsToPostpone for the canonical rationale);
//   - proposer-preferences sets duty.Slot to the future proposal slot the
//     preference targets and emits earlier, near the current slot.
//
// For validator-registration this returns false for both the event-driven
// path (where the deferred-broadcast trade-off applies) and the periodic
// path (where slotDelay would be ~0 anyway) — keeping the role-level check
// simple is preferable to distinguishing the two paths in the duty.
func dutySlotIsExecutionSlot(role types.RunnerRole) bool {
	return role != types.RoleVoluntaryExit &&
		role != types.RoleValidatorRegistration &&
		role != types.RoleProposerPreferences
}
