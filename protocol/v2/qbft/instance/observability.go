package instance

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
	observabilityName      = "github.com/ssvlabs/ssv/protocol/v2/qbft"
	observabilityNamespace = "ssv.validator"
)

type stage string

const (
	proposalStage stage = "proposal"
	prepareStage  stage = "prepare"
	commitStage   stage = "commit"
)

var (
	meter = otel.Meter(observabilityName)

	validatorStageDurationHistogram = observability.NewMetric(
		meter.Float64Histogram(
			metricName("stage.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("validator stage(proposal, prepare, commit) duration"),
			metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...)))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

func stageAttribute(stage stage) attribute.KeyValue {
	return attribute.String("ssv.validator.stage", string(stage))
}

func roleAttribute(role string) attribute.KeyValue {
	return attribute.String("ssv.runner.role", role)
}

func roundAttribute(round qbft.Round) attribute.KeyValue {
	var convertedRound int64 = -1

	uintRound := uint64(round)
	if uintRound <= math.MaxInt64 {
		convertedRound = int64(uintRound)
	}

	return attribute.Int64("ssv.validator.duty.round", convertedRound)
}
