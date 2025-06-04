package runner

import (
	"time"
)

// measurementsStore stores consensus durations
type measurementsStore struct {
	preConsensusStart     time.Time
	consensusStart        time.Time
	postConsensusStart    time.Time
	dutyStart             time.Time
	preConsensusDuration  time.Duration
	consensusDuration     time.Duration
	postConsensusDuration time.Duration
	dutyDuration          time.Duration
}

func NewMeasurementsStore() measurementsStore {
	return measurementsStore{}
}

func (cm *measurementsStore) PreConsensusTime() time.Duration {
	return cm.preConsensusDuration
}
func (cm *measurementsStore) ConsensusTime() time.Duration {
	return cm.consensusDuration
}
func (cm *measurementsStore) PostConsensusTime() time.Duration {
	return cm.postConsensusDuration
}
func (cm *measurementsStore) DutyDurationTime() time.Duration {
	return cm.dutyDuration
}

func (cm *measurementsStore) TotalConsensusTime() time.Duration {
	return cm.preConsensusDuration + cm.consensusDuration + cm.postConsensusDuration
}

// StartPreConsensus stores pre-consensus start time.
func (cm *measurementsStore) StartPreConsensus() {
	if cm != nil {
		cm.preConsensusStart = time.Now()
	}
}

// EndPreConsensus sends metrics for pre-consensus duration.
func (cm *measurementsStore) EndPreConsensus() {
	if cm != nil && !cm.preConsensusStart.IsZero() {
		duration := time.Since(cm.preConsensusStart)
		cm.preConsensusDuration = duration
		cm.preConsensusStart = time.Time{}
	}
}

// StartConsensus stores consensus start time.
func (cm *measurementsStore) StartConsensus() {
	if cm != nil {
		cm.consensusStart = time.Now()
	}
}

// EndConsensus sends metrics for consensus duration.
func (cm *measurementsStore) EndConsensus() {
	if cm != nil && !cm.consensusStart.IsZero() {
		duration := time.Since(cm.consensusStart)
		cm.consensusDuration = duration
		cm.consensusStart = time.Time{}
	}
}

// StartPostConsensus stores post-consensus start time.
func (cm *measurementsStore) StartPostConsensus() {
	if cm != nil {
		cm.postConsensusStart = time.Now()
	}
}

// EndPostConsensus sends metrics for post-consensus duration.
func (cm *measurementsStore) EndPostConsensus() {
	if cm != nil && !cm.postConsensusStart.IsZero() {
		duration := time.Since(cm.postConsensusStart)
		cm.postConsensusDuration = duration
		cm.postConsensusStart = time.Time{}
	}
}

// StartDutyFullFlow stores duty full flow start time.
func (cm *measurementsStore) StartDutyFlow() {
	if cm != nil {
		cm.dutyStart = time.Now()
		cm.dutyDuration = 0
	}
}

// PauseDutyFlow stores duty full flow cumulative duration with ability to continue the flow.
func (cm *measurementsStore) PauseDutyFlow() {
	if cm != nil {
		cm.dutyDuration += time.Since(cm.dutyStart)
		cm.dutyStart = time.Time{}
	}
}

// ContinueDutyFlow continues measuring duty full flow duration.
func (cm *measurementsStore) ContinueDutyFlow() {
	if cm != nil {
		cm.dutyStart = time.Now()
	}
}

// EndDutyFlow sends metrics for duty full flow duration.
func (cm *measurementsStore) EndDutyFlow() {
	if cm != nil && !cm.dutyStart.IsZero() {
		cm.dutyDuration += time.Since(cm.dutyStart)
		cm.dutyStart = time.Time{}
	}
}
