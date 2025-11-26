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
)

type metricsRecorder struct {
	logger *zap.Logger

	// stageStart records the start of some QBFT stage.
	stageStart time.Time
	runnerRole spectypes.RunnerRole
}

func newMetrics(logger *zap.Logger, runnerRole spectypes.RunnerRole) *metricsRecorder {
	return &metricsRecorder{
		logger:     logger,
		runnerRole: runnerRole,
	}
}

// Start records the start of stage(phase) 1 for QBFT instance (it's either a proposal or prepare).
func (m *metricsRecorder) Start() {
	m.stageStart = time.Now()
}

func (m *metricsRecorder) EndStage(ctx context.Context, round specqbft.Round, s stage) {
	took := time.Since(m.stageStart)

	m.logger.Debug("stage finished",
		fields.RunnerRole(m.runnerRole),
		fields.QBFTRound(round),
		zap.String("stage", string(s)),
		fields.Took(took),
	)
	validatorStageDurationHistogram.Record(
		ctx,
		took.Seconds(),
		metric.WithAttributes(
			stageAttribute(s),
			observability.RunnerRoleAttribute(m.runnerRole),
			observability.DutyRoundAttribute(round)),
	)

	m.stageStart = time.Now()
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
