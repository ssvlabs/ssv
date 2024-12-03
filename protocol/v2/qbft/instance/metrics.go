package instance

import (
	"context"
	"math"
	"time"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	"go.opentelemetry.io/otel/metric"
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

func (m *metrics) EndStageProposal(ctx context.Context) {
	validatorStageDurationHistogram.Record(
		ctx,
		time.Since(m.stageStart).Seconds(),
		metric.WithAttributes(stageAttribute(proposalStage)))
	m.stageStart = time.Now()
}

func (m *metrics) EndStagePrepare(ctx context.Context) {
	validatorStageDurationHistogram.Record(
		ctx,
		time.Since(m.stageStart).Seconds(),
		metric.WithAttributes(stageAttribute(prepareStage)))
	m.stageStart = time.Now()
}

func (m *metrics) EndStageCommit(ctx context.Context) {
	validatorStageDurationHistogram.Record(
		ctx,
		time.Since(m.stageStart).Seconds(),
		metric.WithAttributes(stageAttribute(commitStage)))
	m.stageStart = time.Now()
}

func (m *metrics) SetRound(ctx context.Context, round specqbft.Round) {
	convertedRound := uint64(round)
	if convertedRound <= math.MaxInt64 {
		roundGauge.Record(ctx, int64(convertedRound), metric.WithAttributes(roleAttribute(m.role)))
	}
}
