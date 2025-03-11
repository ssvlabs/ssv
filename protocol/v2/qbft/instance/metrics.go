package instance

import (
	"context"
	"time"

	"github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv/observability"
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

func (m *metrics) EndStage(ctx context.Context, round qbft.Round, s stage) {
	validatorStageDurationHistogram.Record(
		ctx,
		time.Since(m.stageStart).Seconds(),
		metric.WithAttributes(
			stageAttribute(s),
			roleAttribute(m.role),
			observability.DutyRoundAttribute(round)))
	m.stageStart = time.Now()
}

// RecordRoundChange records a round change event with the specified reason.
func (m *metrics) RecordRoundChange(ctx context.Context, round qbft.Round, reason roundChangeReason) {
	roundChangesCounter.Add(
		ctx,
		1,
		metric.WithAttributes(
			roleAttribute(m.role),
			observability.DutyRoundAttribute(round),
			reasonAttribute(reason)))
}
