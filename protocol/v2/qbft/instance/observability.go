package instance

import (
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/protocol/v2/qbft"
	observabilityNamespace = "ssv.validator"
)

// stage represents a QBFT protocol stage
type stage string

const (
	stageProposal    stage = "proposal"
	stagePrepare     stage = "prepare"
	stageCommit      stage = "commit"
	stageRoundChange stage = "round_change"
	stageUndefined   stage = "undefined"
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

	validatorStageDurationHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "stage.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("validator stage(proposal, prepare, commit) duration"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	roundsChangedCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "duty.rounds_changed"),
			metric.WithUnit("{change}"),
			metric.WithDescription("number of round changes with their reasons")))

	// committeeInputProposalComparisonCounter is lazily initialized to work around
	// an OTel delegate instrument issue where counters added to an existing var block
	// don't properly register with the Prometheus exporter.
	committeeInputProposalComparisonCounter metric.Int64Counter
	committeeInputProposalComparisonOnce    sync.Once
)

func stageAttribute(stage stage) attribute.KeyValue {
	return attribute.String("ssv.validator.stage", string(stage))
}

func reasonAttribute(reason roundChangeReason) attribute.KeyValue {
	return observability.RoundChangeReasonAttribute(string(reason))
}

func matchAttribute(match bool) attribute.KeyValue {
	return attribute.Bool("ssv.validator.committee.input_proposal_match", match)
}
