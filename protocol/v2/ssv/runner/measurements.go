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

func (dm *dutyMeasurements) PreConsensusTime() time.Duration {
	return dm.preConsensusDuration
}

func (dm *dutyMeasurements) ConsensusTime() time.Duration {
	return dm.consensusDuration
}

func (dm *dutyMeasurements) PostConsensusTime() time.Duration {
	return dm.postConsensusDuration
}

func (dm *dutyMeasurements) TotalConsensusTime() time.Duration {
	return dm.preConsensusDuration + dm.consensusDuration + dm.postConsensusDuration
}

func (dm *dutyMeasurements) TotalDutyTime() time.Duration {
	return dm.dutyDuration
}

func (dm *dutyMeasurements) StartPreConsensus() {
	dm.preConsensusStart = time.Now()
}

func (dm *dutyMeasurements) EndPreConsensus() {
	dm.preConsensusDuration = time.Since(dm.preConsensusStart)
}

func (dm *dutyMeasurements) StartConsensus() {
	dm.consensusStart = time.Now()
}

func (dm *dutyMeasurements) EndConsensus() {
	dm.consensusDuration = time.Since(dm.consensusStart)
}

func (dm *dutyMeasurements) StartPostConsensus() {
	dm.postConsensusStart = time.Now()
}

func (dm *dutyMeasurements) EndPostConsensus() {
	dm.postConsensusDuration = time.Since(dm.postConsensusStart)
}

func (dm *dutyMeasurements) StartDutyFlow() {
	dm.dutyStart = time.Now()
}

func (dm *dutyMeasurements) EndDutyFlow() {
	dm.dutyDuration = time.Since(dm.dutyStart)
}
