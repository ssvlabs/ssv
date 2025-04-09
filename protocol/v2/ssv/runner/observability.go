package runner

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
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
	lock        sync.Mutex
)

var (
	meter = otel.Meter(observabilityName)

	consensusDurationHistogram = observability.NewMetric(
		meter.Float64Histogram(
			metricName("consensus.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("consensus duration"),
			metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...)))

	preConsensusDurationHistogram = observability.NewMetric(
		meter.Float64Histogram(
			metricName("pre_consensus.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("pre consensus duration"),
			metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...)))

	postConsensusDurationHistogram = observability.NewMetric(
		meter.Float64Histogram(
			metricName("post_consensus.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("post consensus duration"),
			metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...)))

	dutyDurationHistogram = observability.NewMetric(
		meter.Float64Histogram(
			metricName("duty.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("duty duration"),
			metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...)))

	submissionsGauge = observability.NewMetric(
		meter.Int64Gauge(
			metricName("submissions"),
			metric.WithUnit("{submission}"),
			metric.WithDescription("number of duty submissions")))

	failedSubmissionCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("submissions.failed"),
			metric.WithUnit("{submission}"),
			metric.WithDescription("total number of failed duty submissions")))
)

func recordSuccessfulSubmission(ctx context.Context, count uint32, epoch phase0.Epoch, role types.BeaconRole) {
	lock.Lock()
	defer lock.Unlock()

	var rolesToReset []types.BeaconRole
	for r, submission := range submissions {
		if submission.epoch != 0 && submission.epoch < epoch {
			submissionsGauge.Record(ctx,
				int64(submission.count),
				metric.WithAttributes(
					observability.BeaconRoleAttribute(r)))
			rolesToReset = append(rolesToReset, r)
		}
	}

	for _, r := range rolesToReset {
		submissions[r] = submissionsMetric{
			epoch: epoch,
		}
	}

	submission := submissions[role]
	submission.epoch = epoch
	submission.count += count
	submissions[role] = submission
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

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}
