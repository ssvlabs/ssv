package instance

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

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
	meter  = otel.Meter(observabilityName)
	tracer = otel.Tracer(observabilityName)

	validatorStageDurationHistogram = observability.NewMetric(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "stage.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("validator stage(proposal, prepare, commit) duration"),
			metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...)))
)

func stageAttribute(stage stage) attribute.KeyValue {
	return attribute.String("ssv.validator.stage", string(stage))
}

func roleAttribute(role string) attribute.KeyValue {
	return attribute.String(observability.RunnerRoleAttrKey, role)
}
