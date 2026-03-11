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

	proposerGetBeaconBlockDurationHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "proposer.get_beacon_block.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("duration of proposer GetBeaconBlock calls (end-to-end, regardless of MEV mode)"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	proposerGetBeaconBlockFinishOffsetHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "proposer.get_beacon_block.finish_offset"),
			metric.WithUnit("s"),
			metric.WithDescription("time since slot start when proposer GetBeaconBlock returned (seconds, regardless of MEV mode)"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	mevDryRunBaselineDurationHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "mev_dry_run.baseline_get_block.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("duration of baseline GetBeaconBlock calls (legacy proposer flow) when MEV dry-run is enabled"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	mevDryRunBaselineFinishOffsetHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "mev_dry_run.baseline_finish_offset"),
			metric.WithUnit("s"),
			metric.WithDescription("time since slot start when baseline GetBeaconBlock finished (seconds)"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	mevDryRunShadowHeadHashDurationHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "mev_dry_run.shadow_get_header.head_hash.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("duration of execution-head (parent_hash) lookup for shadow get_header when MEV dry-run is enabled"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	mevDryRunShadowDurationHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "mev_dry_run.shadow_get_header.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("duration of shadow get_header calls (in-node builder flow) when MEV dry-run is enabled"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	mevDryRunShadowTotalDurationHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "mev_dry_run.shadow_get_header.total_duration"),
			metric.WithUnit("s"),
			metric.WithDescription("duration of shadow get_header total path (head hash lookup + get_header) when MEV dry-run is enabled"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	mevDryRunShadowResultCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "mev_dry_run.shadow_get_header.result"),
			metric.WithUnit("{result}"),
			metric.WithDescription("count of shadow get_header results when MEV dry-run is enabled")))

	mevDryRunParentHashMatchCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "mev_dry_run.parent_hash_match"),
			metric.WithUnit("{match}"),
			metric.WithDescription("count of comparisons where shadow parent_hash matched the baseline execution parent_hash")))

	mevDryRunRecoveredBidCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "mev_dry_run.recovered_bid"),
			metric.WithUnit("{recovered}"),
			metric.WithDescription("count of comparisons where shadow head-parent did not yield a bid but baseline-parent did")))

	mevDryRunShadowExactDurationHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "mev_dry_run.shadow_exact_parent.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("duration of shadow get_header calls using the baseline execution parent_hash"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	mevDryRunShadowExactResultCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "mev_dry_run.shadow_exact_parent.result"),
			metric.WithUnit("{result}"),
			metric.WithDescription("count of shadow exact-parent get_header results")))

	mevDryRunShadowExactFinishOffsetHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "mev_dry_run.shadow_exact_parent.finish_offset"),
			metric.WithUnit("s"),
			metric.WithDescription("time since slot start when exact-parent shadow get_header finished (seconds)"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	mevDryRunShadowMinusBaselineHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "mev_dry_run.shadow_minus_baseline"),
			metric.WithUnit("s"),
			metric.WithDescription("shadow get_header duration minus baseline get_block duration when MEV dry-run is enabled"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	mevDryRunShadowFinishOffsetHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "mev_dry_run.shadow_finish_offset"),
			metric.WithUnit("s"),
			metric.WithDescription("time since slot start when shadow get_header finished (seconds)"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))
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

const (
	proposerGetBeaconBlockResultOK    = "ok"
	proposerGetBeaconBlockResultError = "error"

	proposerGetBeaconBlockBlindedTrue    = "true"
	proposerGetBeaconBlockBlindedFalse   = "false"
	proposerGetBeaconBlockBlindedUnknown = "unknown"
)

func proposerGetBeaconBlockAttributes(result string, blinded string) []attribute.KeyValue {
	if result == "" {
		result = proposerGetBeaconBlockResultError
	}
	if blinded == "" {
		blinded = proposerGetBeaconBlockBlindedUnknown
	}
	return []attribute.KeyValue{
		attribute.String("ssv.runner.proposer.get_beacon_block.result", result),
		attribute.String("ssv.runner.proposer.get_beacon_block.blinded", blinded),
	}
}

func recordProposerGetBeaconBlock(ctx context.Context, result string, blinded string, duration time.Duration, finishOffset time.Duration) {
	if ctx == nil {
		ctx = context.Background()
	}
	attr := proposerGetBeaconBlockAttributes(result, blinded)
	if duration > 0 {
		proposerGetBeaconBlockDurationHistogram.Record(ctx, duration.Seconds(), metric.WithAttributes(attr...))
	}
	if finishOffset > 0 {
		// Negative offsets can happen for future slots; clamp to 0 for clearer dashboards.
		if finishOffset < 0 {
			finishOffset = 0
		}
		proposerGetBeaconBlockFinishOffsetHistogram.Record(ctx, finishOffset.Seconds(), metric.WithAttributes(attr...))
	}
}

func recordMEVDryRunComparison(ctx context.Context, cmp MEVDryRunComparison) {
	shadowResultAttr := attribute.String("ssv.mev.dry_run.shadow_result", cmp.Shadow.Result)
	baselineResultAttr := attribute.String("ssv.mev.dry_run.baseline_result", cmp.Baseline.Result)

	if cmp.Baseline.Took > 0 {
		mevDryRunBaselineDurationHistogram.Record(ctx, cmp.Baseline.Took.Seconds(), metric.WithAttributes(baselineResultAttr))
	}
	if cmp.BaselineFinishOffsetMs > 0 {
		mevDryRunBaselineFinishOffsetHistogram.Record(ctx, float64(cmp.BaselineFinishOffsetMs)/1000.0, metric.WithAttributes(baselineResultAttr))
	}
	if cmp.Shadow.HeadHashTook > 0 {
		mevDryRunShadowHeadHashDurationHistogram.Record(ctx, cmp.Shadow.HeadHashTook.Seconds(), metric.WithAttributes(shadowResultAttr))
	}
	if cmp.Shadow.Took > 0 {
		mevDryRunShadowDurationHistogram.Record(ctx, cmp.Shadow.Took.Seconds(), metric.WithAttributes(shadowResultAttr))
	}
	mevDryRunShadowResultCounter.Add(ctx, 1,
		metric.WithAttributes(shadowResultAttr))

	shadowTotal := cmp.Shadow.HeadHashTook + cmp.Shadow.Took
	if shadowTotal > 0 {
		mevDryRunShadowTotalDurationHistogram.Record(ctx, shadowTotal.Seconds(), metric.WithAttributes(shadowResultAttr))
	}
	if cmp.Baseline.Took > 0 && shadowTotal > 0 {
		mevDryRunShadowMinusBaselineHistogram.Record(ctx, (shadowTotal - cmp.Baseline.Took).Seconds(), metric.WithAttributes(shadowResultAttr))
	}
	if cmp.ShadowFinishOffsetMs > 0 {
		mevDryRunShadowFinishOffsetHistogram.Record(ctx, float64(cmp.ShadowFinishOffsetMs)/1000.0, metric.WithAttributes(shadowResultAttr))
	}

	if cmp.BaselineExecParentHash != "" && cmp.Shadow.ParentHashHex != "" {
		mevDryRunParentHashMatchCounter.Add(ctx, 1,
			metric.WithAttributes(
				attribute.Bool("ssv.mev.dry_run.parent_hash_match", cmp.ParentHashMatch),
				shadowResultAttr,
			))
	}

	if cmp.ShadowExactParent != nil {
		exactResultAttr := attribute.String("ssv.mev.dry_run.shadow_exact_parent_result", cmp.ShadowExactParent.Result)
		if cmp.ShadowExactParent.Took > 0 {
			mevDryRunShadowExactDurationHistogram.Record(ctx, cmp.ShadowExactParent.Took.Seconds(), metric.WithAttributes(exactResultAttr))
		}
		mevDryRunShadowExactResultCounter.Add(ctx, 1, metric.WithAttributes(exactResultAttr))
		if cmp.ShadowExactFinishOffsetMs > 0 {
			mevDryRunShadowExactFinishOffsetHistogram.Record(ctx, float64(cmp.ShadowExactFinishOffsetMs)/1000.0, metric.WithAttributes(exactResultAttr))
		}
	}

	if cmp.RecoveredBid {
		mevDryRunRecoveredBidCounter.Add(ctx, 1, metric.WithAttributes(shadowResultAttr))
	}
}
