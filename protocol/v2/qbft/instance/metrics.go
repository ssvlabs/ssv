package instance

import (
	"context"
	"time"

	"github.com/ssvlabs/ssv-spec/qbft"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
)

type metrics struct {
	stageStart time.Time
	role       string
}

func newMetrics(role string) *metrics {
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
			roleAttribute(m.role),
			observability.DutyRoundAttribute(round)))
	m.stageStart = time.Now()
}

func (m *metrics) EndStagePrepare(ctx context.Context, round qbft.Round) {
	validatorStageDurationHistogram.Record(
		ctx,
		time.Since(m.stageStart).Seconds(),
		metric.WithAttributes(
			stageAttribute(prepareStage),
			roleAttribute(m.role),
			observability.DutyRoundAttribute(round)))
	m.stageStart = time.Now()
}

func (m *metrics) EndStageCommit(ctx context.Context, round qbft.Round) {
	validatorStageDurationHistogram.Record(
		ctx,
		time.Since(m.stageStart).Seconds(),
		metric.WithAttributes(
			stageAttribute(commitStage),
			roleAttribute(m.role),
			observability.DutyRoundAttribute(round)))
	m.stageStart = time.Now()
}
