package runner

import (
	"context"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/protocol/v2/ssv"
	observabilityNamespace = "ssv.validator"
)

type submissionsMetric struct {
	count uint32
	epoch phase0.Epoch
}

var (
	submissions = make(map[types.BeaconRole]submissionsMetric)
	quorums     = make(map[types.BeaconRole]submissionsMetric)

	submissionsLock, quorumsLock sync.Mutex
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
	epochRecordGaugeMetric(ctx, &submissionsLock, submissions, submissionsGauge, count, epoch, role)
}

func recordSuccessfulQuorum(ctx context.Context, count uint32, epoch phase0.Epoch, role types.BeaconRole) {
	epochRecordGaugeMetric(ctx, &quorumsLock, quorums, quorumsGauge, count, epoch, role)
}

func epochRecordGaugeMetric(
	ctx context.Context,
	lock *sync.Mutex,
	metricsMap map[types.BeaconRole]submissionsMetric,
	gauge metric.Int64Gauge,
	count uint32,
	epoch phase0.Epoch,
	role types.BeaconRole,
) {
	lock.Lock()
	defer lock.Unlock()

	var rolesToReset []types.BeaconRole
	for r, entry := range metricsMap {
		if entry.epoch != 0 && entry.epoch < epoch {
			gauge.Record(ctx, int64(entry.count), metric.WithAttributes(observability.BeaconRoleAttribute(r)))

			rolesToReset = append(rolesToReset, r)
		}
	}

	for _, r := range rolesToReset {
		metricsMap[r] = submissionsMetric{
			epoch: epoch,
		}
	}

	entry := metricsMap[role]
	entry.epoch = epoch
	entry.count += count
	metricsMap[role] = entry
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
