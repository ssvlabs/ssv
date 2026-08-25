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
	meter = otel.Meter(observabilityName)

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

	dutyOutcomeCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "duty.outcome"),
			metric.WithUnit("{duty}"),
			metric.WithDescription("total number of concluded duties, by outcome")))

	proposalBuildSourceCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "proposal.build_source"),
			metric.WithUnit("{proposal}"),
			metric.WithDescription("submitted Gloas block proposals by build source (self-build vs builder)")))

	envelopeBuildMatchCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "envelope.build_match"),
			metric.WithUnit("{envelope}"),
			metric.WithDescription("decided Gloas execution-payload envelopes by whether this operator is the one that built them")))

	requestAuthReconstructionCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "request_auth.reconstructions"),
			metric.WithUnit("{root}"),
			metric.WithDescription("threshold-reconstructed Gloas direct-builder request-auth signing roots (issue #2962); token-sharing builders share a root and count once")))

	requestAuthUnavailableCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "request_auth.unavailable"),
			metric.WithUnit("{builder}"),
			metric.WithDescription("configured Gloas direct-builders with no reconstructed request-auth at §4 produce time (issue #2962 E1); omitted from the produceBlockV4 body, degrading to the enshrined flow")))

	builderPreferencesSubmitCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "builder_preferences.submits"),
			metric.WithUnit("{submit}"),
			metric.WithDescription("ahead-of-time Gloas builder-preferences submissions to the beacon node (issue #2962 phase 3), by outcome")))
)

func recordSuccessfulSubmission(ctx context.Context, count int64, epoch phase0.Epoch, role spectypes.BeaconRole) {
	submissions.Record(ctx, count, epoch, role)
}

func recordFailedSubmission(ctx context.Context, role spectypes.BeaconRole) {
	failedSubmissionCounter.Add(ctx, 1, metric.WithAttributes(observability.BeaconRoleAttribute(role)))
}

func recordDutyOutcome(ctx context.Context, role spectypes.RunnerRole, outcome dutyOutcome) {
	dutyOutcomeCounter.Add(ctx, 1,
		metric.WithAttributes(
			observability.RunnerRoleAttribute(role),
			observability.DutyOutcomeAttribute(string(outcome)),
		))
}

// proposalBuildSource is a submitted Gloas proposal's build source (issue #2962 E1): whether the decided
// bid commits to an external builder or to self-build. The decided block cannot reveal why the BN
// self-built (economics vs. a builder being unreachable), so the auth-unavailable dimension is surfaced
// separately, at produce time, by recordProposalAuthUnavailable — a configured builder with no auth this
// slot is a concrete, countable cause independent of this outcome classification.
type proposalBuildSource string

const (
	// buildSourceBuilder — the decided bid commits to an external builder.
	buildSourceBuilder proposalBuildSource = "builder"
	// buildSourceLocal — the decided bid commits to BUILDER_INDEX_SELF_BUILD.
	buildSourceLocal proposalBuildSource = "local"
)

// recordProposalBuildSource counts a submitted Gloas proposal by build source. Gloas-only: the
// decided bid is the same for every operator, unlike the pre-Gloas Blinded flag, which the
// distributed submit skews.
func recordProposalBuildSource(ctx context.Context, source proposalBuildSource) {
	proposalBuildSourceCounter.Add(ctx, 1, metric.WithAttributes(observability.BuildSourceAttribute(string(source))))
}

// recordEnvelopeBuildMatch counts a decided §6 envelope by whether this operator's cached envelope
// content-matches it ("self") or not ("other"). Only the matching operator holds the full payload
// bytes and publishes, so per operator an "other" share is expected and benign — the signal is
// cluster-wide: a decided envelope no operator matched is a reconstruction miss (the builder's bytes
// were lost and nobody can publish), which this makes countable instead of inferable only from the
// absence of a publish log. Deliberately independent of whether the subsequent submit succeeded —
// that failure is already counted by ssv.runner.submissions.failed.
func recordEnvelopeBuildMatch(ctx context.Context, self bool) {
	match := "other"
	if self {
		match = "self"
	}
	envelopeBuildMatchCounter.Add(ctx, 1, metric.WithAttributes(observability.EnvelopeBuildMatchAttribute(match)))
}

// recordRequestAuthReconstruction counts a threshold-reconstructed request-auth signing root
// (issue #2962; token-sharing builders share a root and count once). Its inverse — an auth that never
// reached quorum — is measured where it bites, by recordProposalAuthUnavailable at the §4 produce path.
func recordRequestAuthReconstruction(ctx context.Context) {
	requestAuthReconstructionCounter.Add(ctx, 1)
}

// recordProposalAuthUnavailable counts configured direct-builders that had no reconstructed request-auth
// for the slot at §4 produce time (issue #2962 E1) — the inverse of recordRequestAuthReconstruction and
// the auth dimension of the build-source telemetry: these builders are omitted from the produceBlockV4
// body, so the proposal silently degrades to gossiped bids / self-build for them.
func recordProposalAuthUnavailable(ctx context.Context, count int) {
	requestAuthUnavailableCounter.Add(ctx, int64(count))
}

// recordBuilderPreferencesSubmit counts an ahead-of-time builder-preferences submission (issue #2962
// phase 3) by outcome. It is best-effort at the caller, so a failure is a health signal, not a duty failure.
func recordBuilderPreferencesSubmit(ctx context.Context, success bool) {
	outcome := "failure"
	if success {
		outcome = "success"
	}
	builderPreferencesSubmitCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
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
