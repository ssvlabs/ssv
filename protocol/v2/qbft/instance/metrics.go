package instance

import (
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

type Metrics interface {
	QBFTProposalDuration(duration time.Duration)
	QBFTPrepareDuration(duration time.Duration)
	QBFTCommitDuration(duration time.Duration)
	QBFTRound(msgID spectypes.MessageID, round specqbft.Round)
}

type metricsSubmitter struct {
	metrics    Metrics
	stageStart time.Time
	msgID      spectypes.MessageID
}

func newMetricsSubmitter(msgID spectypes.MessageID, metrics Metrics) *metricsSubmitter {
	return &metricsSubmitter{
		msgID:   msgID,
		metrics: metrics,
	}
}

func (m *metricsSubmitter) StartStage() {
	m.stageStart = time.Now()
}

func (m *metricsSubmitter) EndStageProposal() {
	m.metrics.QBFTProposalDuration(time.Since(m.stageStart))
	m.stageStart = time.Now()
}

func (m *metricsSubmitter) EndStagePrepare() {
	m.metrics.QBFTPrepareDuration(time.Since(m.stageStart))
	m.stageStart = time.Now()
}

func (m *metricsSubmitter) EndStageCommit() {
	m.metrics.QBFTCommitDuration(time.Since(m.stageStart))
	m.stageStart = time.Now()
}

func (m *metricsSubmitter) SetRound(round specqbft.Round) {
	m.metrics.QBFTRound(m.msgID, round)
}
