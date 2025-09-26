package runner

import (
	"time"
)

// dutyMeasurements stores duty-related & consensus-related durations.
type dutyMeasurements struct {
	preConsensusStart     time.Time
	consensusStart        time.Time
	postConsensusStart    time.Time
	dutyStart             time.Time
	preConsensusDuration  time.Duration
	consensusDuration     time.Duration
	postConsensusDuration time.Duration
	dutyDuration          time.Duration
}

func newMeasurementsStore() *dutyMeasurements {
	return &dutyMeasurements{}
}

func (cm *dutyMeasurements) PreConsensusTime() time.Duration {
	return cm.preConsensusDuration
}

func (cm *dutyMeasurements) ConsensusTime() time.Duration {
	return cm.consensusDuration
}

func (cm *dutyMeasurements) PostConsensusTime() time.Duration {
	return cm.postConsensusDuration
}

func (cm *dutyMeasurements) TotalConsensusTime() time.Duration {
	return cm.preConsensusDuration + cm.consensusDuration + cm.postConsensusDuration
}

func (cm *dutyMeasurements) TotalDutyTime() time.Duration {
	return cm.dutyDuration
}

func (cm *dutyMeasurements) StartPreConsensus() {
	if cm != nil {
		cm.preConsensusStart = time.Now()
	}
}

func (cm *dutyMeasurements) EndPreConsensus() {
	if cm != nil {
		cm.preConsensusDuration = time.Since(cm.preConsensusStart)
	}
}

func (cm *dutyMeasurements) StartConsensus() {
	if cm != nil {
		cm.consensusStart = time.Now()
	}
}

func (cm *dutyMeasurements) EndConsensus() {
	if cm != nil {
		cm.consensusDuration = time.Since(cm.consensusStart)
	}
}

func (cm *dutyMeasurements) StartPostConsensus() {
	if cm != nil {
		cm.postConsensusStart = time.Now()
	}
}

func (cm *dutyMeasurements) EndPostConsensus() {
	if cm != nil {
		cm.postConsensusDuration = time.Since(cm.postConsensusStart)
	}
}

func (cm *dutyMeasurements) StartDutyFlow() {
	if cm != nil {
		cm.dutyStart = time.Now()
	}
}

func (cm *dutyMeasurements) EndDutyFlow() {
	if cm != nil {
		cm.dutyDuration = time.Since(cm.dutyStart)
	}
}
