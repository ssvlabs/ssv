package runner

import (
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

type Metrics interface {
	ConsensusDutySubmission(role spectypes.BeaconRole, duration time.Duration)
	ConsensusDuration(role spectypes.BeaconRole, duration time.Duration)
	PreConsensusDuration(role spectypes.BeaconRole, duration time.Duration)
	PostConsensusDuration(role spectypes.BeaconRole, duration time.Duration)
	DutyFullFlowDuration(role spectypes.BeaconRole, duration time.Duration)
	DutyFullFlowFirstRoundDuration(role spectypes.BeaconRole, duration time.Duration)
	RoleSubmitted(role spectypes.BeaconRole)
	RoleSubmissionFailure(role spectypes.BeaconRole)
	InstanceStarted(role spectypes.BeaconRole)
	InstanceDecided(role spectypes.BeaconRole)
}

type consensusMetricsSubmitter struct {
	metrics                        Metrics
	role                           spectypes.BeaconRole
	preConsensusStart              time.Time
	consensusStart                 time.Time
	postConsensusStart             time.Time
	dutyFullFlowStart              time.Time
	dutyFullFlowCumulativeDuration time.Duration
}

func newConsensusMetricsSubmitter(metrics Metrics, role spectypes.BeaconRole) consensusMetricsSubmitter {
	return consensusMetricsSubmitter{
		metrics: metrics,
		role:    role,
	}
}

// StartPreConsensus stores pre-consensus start time.
func (cm *consensusMetricsSubmitter) StartPreConsensus() {
	if cm != nil {
		cm.preConsensusStart = time.Now()
	}
}

// EndPreConsensus sends metrics for pre-consensus duration.
func (cm *consensusMetricsSubmitter) EndPreConsensus() {
	if !cm.preConsensusStart.IsZero() {
		cm.metrics.PreConsensusDuration(cm.role, time.Since(cm.preConsensusStart))
		cm.preConsensusStart = time.Time{}
	}
}

// StartConsensus stores consensus start time.
func (cm *consensusMetricsSubmitter) StartConsensus() {
	if cm != nil {
		cm.consensusStart = time.Now()
		cm.metrics.InstanceStarted(cm.role)
	}
}

// EndConsensus sends metrics for consensus duration.
func (cm *consensusMetricsSubmitter) EndConsensus() {
	if !cm.consensusStart.IsZero() {
		cm.metrics.ConsensusDuration(cm.role, time.Since(cm.consensusStart))
		cm.consensusStart = time.Time{}
		cm.metrics.InstanceDecided(cm.role)
	}
}

// StartPostConsensus stores post-consensus start time.
func (cm *consensusMetricsSubmitter) StartPostConsensus() {
	if cm != nil {
		cm.postConsensusStart = time.Now()
	}
}

// EndPostConsensus sends metrics for post-consensus duration.
func (cm *consensusMetricsSubmitter) EndPostConsensus() {
	if !cm.postConsensusStart.IsZero() {
		cm.metrics.PostConsensusDuration(cm.role, time.Since(cm.postConsensusStart))
		cm.postConsensusStart = time.Time{}
	}
}

// StartDutyFullFlow stores duty full flow start time.
func (cm *consensusMetricsSubmitter) StartDutyFullFlow() {
	if cm != nil {
		cm.dutyFullFlowStart = time.Now()
		cm.dutyFullFlowCumulativeDuration = 0
	}
}

// PauseDutyFullFlow stores duty full flow cumulative duration with ability to continue the flow.
func (cm *consensusMetricsSubmitter) PauseDutyFullFlow() {
	if cm != nil {
		cm.dutyFullFlowCumulativeDuration += time.Since(cm.dutyFullFlowStart)
		cm.dutyFullFlowStart = time.Time{}
	}
}

// ContinueDutyFullFlow continues measuring duty full flow duration.
func (cm *consensusMetricsSubmitter) ContinueDutyFullFlow() {
	if cm != nil {
		cm.dutyFullFlowStart = time.Now()
	}
}

// EndDutyFullFlow sends metrics for duty full flow duration.
func (cm *consensusMetricsSubmitter) EndDutyFullFlow(round specqbft.Round) {
	if !cm.dutyFullFlowStart.IsZero() {
		cm.dutyFullFlowCumulativeDuration += time.Since(cm.dutyFullFlowStart)
		cm.metrics.DutyFullFlowDuration(cm.role, cm.dutyFullFlowCumulativeDuration)

		if round == 1 {
			cm.metrics.DutyFullFlowFirstRoundDuration(cm.role, cm.dutyFullFlowCumulativeDuration)
		}

		cm.dutyFullFlowStart = time.Time{}
		cm.dutyFullFlowCumulativeDuration = 0
	}
}

// StartBeaconSubmission returns a function that sends metrics for beacon submission duration.
func (cm *consensusMetricsSubmitter) StartBeaconSubmission() (endBeaconSubmission func()) {
	start := time.Now()
	return func() {
		cm.metrics.ConsensusDutySubmission(cm.role, time.Since(start))
	}
}

// RoleSubmitted increases submitted roles counter.
func (cm *consensusMetricsSubmitter) RoleSubmitted() {
	cm.metrics.RoleSubmitted(cm.role)
}

// RoleSubmissionFailed increases non-submitted roles counter.
func (cm *consensusMetricsSubmitter) RoleSubmissionFailed() {
	cm.metrics.RoleSubmissionFailure(cm.role)
}
