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

// stage represents a QBFT protocol stage
type stage string

const (
	stageProposal stage = "proposal"
	stagePrepare  stage = "prepare"
	stageCommit   stage = "commit"
)

// roundChangeReason represents the reason for a round change in the QBFT protocol
type roundChangeReason string

const (
	reasonTimeout       roundChangeReason = "timeout"
	reasonPartialQuorum roundChangeReason = "partial-quorum"
	reasonJustified     roundChangeReason = "justified"
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

	roundsChangedCounter = observability.NewMetric(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "duty.rounds_changed"),
			metric.WithUnit("{change}"),
			metric.WithDescription("number of round changes with their reasons")))
)

func stageAttribute(stage stage) attribute.KeyValue {
	return attribute.String("ssv.validator.stage", string(stage))
}

func roleAttribute(role string) attribute.KeyValue {
	return attribute.String(observability.RunnerRoleAttrKey, role)
}

func reasonAttribute(reason roundChangeReason) attribute.KeyValue {
	return observability.RoundChangeReasonAttribute(string(reason))
}
