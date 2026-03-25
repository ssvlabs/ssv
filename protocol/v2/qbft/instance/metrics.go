package instance

import (
	"context"
	"time"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/observability/metrics"
)

type metricsRecorder struct {
	logger *zap.Logger

	runnerRole spectypes.RunnerRole

	// stage is the current stage of some QBFT instance.
	stage stage

	// stageStart records the start of some QBFT stage.
	stageStart time.Time
}

func newMetrics(logger *zap.Logger, runnerRole spectypes.RunnerRole) *metricsRecorder {
	return &metricsRecorder{
		logger:     logger,
		runnerRole: runnerRole,
		stage:      stageUndefined,
	}
}

// StartStage records the start of a stage(phase) for QBFT instance.
func (m *metricsRecorder) StartStage(s stage) {
	m.stage = s
	m.stageStart = time.Now()
}

func (m *metricsRecorder) EndStage(ctx context.Context, round specqbft.Round) {
	took := time.Since(m.stageStart)

	m.logger.Debug("stage finished",
		fields.QBFTRound(round),
		zap.String("stage", string(m.stage)),
		fields.Took(took),
	)

	validatorStageDurationHistogram.Record(
		ctx,
		took.Seconds(),
		metric.WithAttributes(
			stageAttribute(m.stage),
			observability.RunnerRoleAttribute(m.runnerRole),
			observability.DutyRoundAttribute(round)),
	)

	m.stage = stageUndefined
}

// RecordRoundChange records a round change event with the specified reason.
func (m *metricsRecorder) RecordRoundChange(ctx context.Context, round specqbft.Round, reason roundChangeReason) {
	roundsChangedCounter.Add(
		ctx,
		1,
		metric.WithAttributes(
			observability.RunnerRoleAttribute(m.runnerRole),
			observability.DutyRoundAttribute(round),
			reasonAttribute(reason)),
	)
}

func (m *metricsRecorder) RecordCommitteeInputProposalComparison(ctx context.Context, match bool) {
	committeeInputProposalComparisonOnce.Do(func() {
		committeeInputProposalComparisonCounter = metrics.New(
			meter.Int64Counter(
				observability.InstrumentName(observabilityNamespace, "committee.input_proposal_comparisons"),
				metric.WithUnit("{observation}"),
				metric.WithDescription("number of committee input and proposal value comparisons")))
	})
	committeeInputProposalComparisonCounter.Add(
		ctx,
		1,
		metric.WithAttributes(
			observability.RunnerRoleAttribute(m.runnerRole),
			matchAttribute(match),
		),
	)
}
