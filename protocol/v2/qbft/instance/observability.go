package instance

import (
	"fmt"

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
	proposalStage stage = "proposal"
	prepareStage  stage = "prepare"
	commitStage   stage = "commit"
)

// roundChangeReason represents the reason for a round change in the QBFT protocoltu
type roundChangeReason string

const (
	timeoutReason       roundChangeReason = "timeout"
	partialQuorumReason roundChangeReason = "partial-quorum"
	receivedReason      roundChangeReason = "received"
	justifiedReason     roundChangeReason = "justified"
)

var (
	meter = otel.Meter(observabilityName)

	validatorStageDurationHistogram = observability.NewMetric(
		meter.Float64Histogram(
			metricName("stage.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("validator stage(proposal, prepare, commit) duration"),
			metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...)))

	roundChangesCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("round.changes"),
			metric.WithUnit("{change}"),
			metric.WithDescription("number of round changes with their reasons")))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

func stageAttribute(stage stage) attribute.KeyValue {
	return attribute.String("ssv.validator.stage", string(stage))
}

func roleAttribute(role string) attribute.KeyValue {
	return attribute.String(observability.RunnerRoleAttrKey, role)
}

func reasonAttribute(reason roundChangeReason) attribute.KeyValue {
	return observability.RoundChangeReasonAttribute(string(reason))
}
