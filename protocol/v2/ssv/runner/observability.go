package runner

import (
	"context"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"
)

type attributeConsensusPhase string

const (
	observabilityName      = "github.com/ssvlabs/ssv/protocol/v2/ssv"
	observabilityNamespace = "ssv.validator"

	attributeConsensusPhasePreConsensus attributeConsensusPhase = "pre_consensus"
)

type (
	EpochMetricRecorder struct {
		mu    sync.Mutex
		data  map[types.BeaconRole]epochCounter
		gauge metric.Int64Gauge
	}

	epochCounter struct {
		count uint32
		epoch phase0.Epoch
	}
)

func (r *EpochMetricRecorder) Record(ctx context.Context, count uint32, epoch phase0.Epoch, role types.BeaconRole, attributes ...attribute.KeyValue) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var rolesToReset []types.BeaconRole

	for rKey, entry := range r.data {
		if entry.epoch != 0 && entry.epoch < epoch {
			r.gauge.Record(ctx, int64(entry.count), metric.WithAttributes(attributes...))
			rolesToReset = append(rolesToReset, rKey)
		}
	}

	for _, rKey := range rolesToReset {
		r.data[rKey] = epochCounter{epoch: epoch}
	}

	entry := r.data[role]
	entry.epoch = epoch
	entry.count += count
	r.data[role] = entry
}

var (
	submissions = EpochMetricRecorder{
		data:  make(map[types.BeaconRole]epochCounter),
		gauge: submissionsGauge,
	}
	quorums = EpochMetricRecorder{
		data:  make(map[types.BeaconRole]epochCounter),
		gauge: quorumsGauge,
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

	quorumsGauge = metrics.New(
		meter.Int64Gauge(
			observability.InstrumentName(observabilityNamespace, "quorums"),
			metric.WithUnit("{quorum}"),
			metric.WithDescription("number of successful quorums")))

	failedSubmissionCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "submissions.failed"),
			metric.WithUnit("{submission}"),
			metric.WithDescription("total number of failed duty submissions")))
)

func recordSuccessfulSubmission(ctx context.Context, count uint32, epoch phase0.Epoch, role types.BeaconRole) {
	submissions.Record(ctx, count, epoch, role, observability.BeaconRoleAttribute(role))
}

func recordSuccessfulQuorum(ctx context.Context, count uint32, epoch phase0.Epoch, role types.BeaconRole, phase attributeConsensusPhase) {
	attributes := []attribute.KeyValue{
		observability.BeaconRoleAttribute(role),
		attribute.String("ssv.validator.duty.phase", string(phase)),
	}
	quorums.Record(ctx, count, epoch, role, attributes...)
}

func recordFailedSubmission(ctx context.Context, role types.BeaconRole) {
	failedSubmissionCounter.Add(ctx, 1, metric.WithAttributes(observability.BeaconRoleAttribute(role)))
}

func recordConsensusDuration(ctx context.Context, duration time.Duration, role types.RunnerRole) {
	consensusDurationHistogram.Record(ctx, duration.Seconds(),
		metric.WithAttributes(
			observability.RunnerRoleAttribute(role),
		))
}

func recordPreConsensusDuration(ctx context.Context, duration time.Duration, role types.RunnerRole) {
	preConsensusDurationHistogram.Record(ctx, duration.Seconds(),
		metric.WithAttributes(
			observability.RunnerRoleAttribute(role),
		))
}

func recordPostConsensusDuration(ctx context.Context, duration time.Duration, role types.RunnerRole) {
	postConsensusDurationHistogram.Record(ctx, duration.Seconds(),
		metric.WithAttributes(
			observability.RunnerRoleAttribute(role),
		))
}

func recordDutyDuration(ctx context.Context, duration time.Duration, role types.BeaconRole, round qbft.Round) {
	dutyDurationHistogram.Record(ctx, duration.Seconds(),
		metric.WithAttributes(
			observability.BeaconRoleAttribute(role),
			observability.DutyRoundAttribute(round),
		))
}
