package runner

import (
	"context"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
	observabilityNamespace = "ssv.runner"
)

type (
	// EpochMetricRecorder records gauge metrics on an epoch-by-epoch basis for different BeaconRoles.
	// It tracks counts and the latest epoch for each role, and ensures metrics are flushed when the epoch advances.
	// This allows periodic metric reporting aligned with epoch boundaries.
	EpochMetricRecorder struct {
		mu    sync.Mutex
		data  map[spectypes.BeaconRole]epochCounter
		gauge metric.Int64Gauge
	}

	epochCounter struct {
		count int64
		epoch phase0.Epoch
	}
)

// Record updates and reports the gauge metric for a given BeaconRole and epoch.
// When the epoch advances, all roles from the internal data map that had recorded duties
// in the previous epoch will have their metrics flushed (recorded), not just the role passed in.
// This is necessary because not all duties are executed every epoch, so to ensure accurate
// metric reporting, all completed roles from the previous epoch must be recorded once the new epoch begins.
// The method automatically appends the relevant beacon role attribute to each metric entry
// and does not require the caller to explicitly include it in the `attributes` slice.
func (r *EpochMetricRecorder) Record(ctx context.Context, count int64, epoch phase0.Epoch, beaconRole spectypes.BeaconRole, attributes ...attribute.KeyValue) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var rolesToReset []spectypes.BeaconRole

	for role, entry := range r.data {
		if entry.epoch != 0 && entry.epoch < epoch {
			attr := append([]attribute.KeyValue{observability.BeaconRoleAttribute(role)}, attributes...)
			r.gauge.Record(ctx, entry.count, metric.WithAttributes(attr...))

			rolesToReset = append(rolesToReset, role)
		}
	}

	for _, role := range rolesToReset {
		r.data[role] = epochCounter{epoch: epoch}
	}

	entry := r.data[beaconRole]
	entry.epoch = epoch
	entry.count += count
	r.data[beaconRole] = entry
}

var (
	submissions = EpochMetricRecorder{
		data:  make(map[spectypes.BeaconRole]epochCounter),
		gauge: submissionsGauge,
	}
)

var (
	tracer = otel.Tracer(observabilityName)
	meter  = otel.Meter(observabilityName)

	consensusDurationHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "consensus.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("consensus duration"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	preConsensusDurationHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "pre_consensus.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("pre consensus duration"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	postConsensusDurationHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "post_consensus.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("post consensus duration"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	dutyDurationHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "duty.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("duty duration"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	submissionsGauge = metrics.New(
		meter.Int64Gauge(
			observability.InstrumentName(observabilityNamespace, "submissions"),
			metric.WithUnit("{submission}"),
			metric.WithDescription("number of duty submissions")))

	failedSubmissionCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "submissions.failed"),
			metric.WithUnit("{submission}"),
			metric.WithDescription("total number of failed duty submissions")))
)

func recordSuccessfulSubmission(ctx context.Context, count int64, epoch phase0.Epoch, role spectypes.BeaconRole) {
	submissions.Record(ctx, count, epoch, role)
}

func recordFailedSubmission(ctx context.Context, role spectypes.BeaconRole) {
	failedSubmissionCounter.Add(ctx, 1, metric.WithAttributes(observability.BeaconRoleAttribute(role)))
}

func recordPreConsensusDuration(ctx context.Context, duration time.Duration, role spectypes.RunnerRole) {
	preConsensusDurationHistogram.Record(ctx, duration.Seconds(),
		metric.WithAttributes(
			observability.RunnerRoleAttribute(role),
		))
}

func recordConsensusDuration(ctx context.Context, duration time.Duration, role spectypes.RunnerRole) {
	consensusDurationHistogram.Record(ctx, duration.Seconds(),
		metric.WithAttributes(
			observability.RunnerRoleAttribute(role),
		))
}

func recordPostConsensusDuration(ctx context.Context, duration time.Duration, role spectypes.RunnerRole) {
	postConsensusDurationHistogram.Record(ctx, duration.Seconds(),
		metric.WithAttributes(
			observability.RunnerRoleAttribute(role),
		))
}

func recordTotalDutyDuration(ctx context.Context, duration time.Duration, role spectypes.RunnerRole, round specqbft.Round) {
	dutyDurationHistogram.Record(ctx, duration.Seconds(),
		metric.WithAttributes(
			observability.RunnerRoleAttribute(role),
			observability.DutyRoundAttribute(round),
		))
}
