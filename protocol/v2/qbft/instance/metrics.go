package instance

import (
	"context"
	"time"

	"github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
)

type metricsRecorder struct {
	// stageStart records the start of some QBFT stage.
	stageStart time.Time
	runnerRole spectypes.RunnerRole
}

func newMetrics(runnerRole spectypes.RunnerRole) *metricsRecorder {
	return &metricsRecorder{
		runnerRole: runnerRole,
	}
}

// Start records the start of stage(phase) 1 for QBFT instance (it's either a proposal or prepare).
func (m *metricsRecorder) Start() {
	m.stageStart = time.Now()
}

func (m *metricsRecorder) EndStage(ctx context.Context, round qbft.Round, s stage) {
	validatorStageDurationHistogram.Record(
		ctx,
		time.Since(m.stageStart).Seconds(),
		metric.WithAttributes(
			stageAttribute(s),
			observability.RunnerRoleAttribute(m.runnerRole),
			observability.DutyRoundAttribute(round)),
	)
	m.stageStart = time.Now()
}

// RecordRoundChange records a round change event with the specified reason.
func (m *metricsRecorder) RecordRoundChange(ctx context.Context, round qbft.Round, reason roundChangeReason) {
	roundsChangedCounter.Add(
		ctx,
		1,
		metric.WithAttributes(
			observability.RunnerRoleAttribute(m.runnerRole),
			observability.DutyRoundAttribute(round),
			reasonAttribute(reason)),
	)
}
