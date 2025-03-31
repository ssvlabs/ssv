package instance

import (
	"context"
	"github.com/ssvlabs/ssv-spec/types"
	"time"

	"github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv/observability"
	"go.opentelemetry.io/otel/metric"
)

type metrics struct {
	stageStart time.Time
	role       types.RunnerRole
}

func newMetrics(role types.RunnerRole) *metrics {
	return &metrics{
		role: role,
	}
}

func (m *metrics) StartStage() {
	m.stageStart = time.Now()
}

func (m *metrics) EndStageProposal(ctx context.Context, round qbft.Round) {
	validatorStageDurationHistogram.Record(
		ctx,
		time.Since(m.stageStart).Seconds(),
		metric.WithAttributes(
			stageAttribute(proposalStage),
			observability.RunnerRoleAttribute(m.role),
			observability.DutyRoundAttribute(round)))
	m.stageStart = time.Now()
}

func (m *metrics) EndStagePrepare(ctx context.Context, round qbft.Round) {
	validatorStageDurationHistogram.Record(
		ctx,
		time.Since(m.stageStart).Seconds(),
		metric.WithAttributes(
			stageAttribute(prepareStage),
			observability.RunnerRoleAttribute(m.role),
			observability.DutyRoundAttribute(round)))
	m.stageStart = time.Now()
}

func (m *metrics) EndStageCommit(ctx context.Context, round qbft.Round) {
	validatorStageDurationHistogram.Record(
		ctx,
		time.Since(m.stageStart).Seconds(),
		metric.WithAttributes(
			stageAttribute(commitStage),
			observability.RunnerRoleAttribute(m.role),
			observability.DutyRoundAttribute(round)))
	m.stageStart = time.Now()
}
