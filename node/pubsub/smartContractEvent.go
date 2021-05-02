package pubsub

import (
	"github.com/bloxapp/ssv/pubsub"
	"go.uber.org/zap"
)

// SmartContractEvent obserser for eth1 service
type SmartContractEvent struct {
	pubsub.BaseObserver
}

// Update listen to subject notify
func (s *SmartContractEvent) Update(vLog interface{}) {
	s.Logger.Debug("Got log from contract", zap.Any("log", vLog)) // pointer to Event log
}

// GetID get the observer id
func (s *SmartContractEvent) GetID() string {
	return ""
}
