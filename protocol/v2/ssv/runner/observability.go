package runner

import (
	"fmt"
	"math"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv/observability"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/protocol/v2/ssv"
	observabilityNamespace = "ssv.validator"
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

	submissionCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("submissions"),
			metric.WithUnit("{submission}"),
			metric.WithDescription("total number of duty submissions")))

	failedSubmissionCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("submissions.failed"),
			metric.WithUnit("{submission}"),
			metric.WithDescription("total number of failed duty submissions")))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

func roundAttribute(qbftRound qbft.Round) attribute.KeyValue {
	var round int64
	r := uint64(qbftRound)
	if r <= math.MaxInt64 {
		round = int64(r)
	}
	return attribute.Int64("ssv.validator.duty.round", round)
}
