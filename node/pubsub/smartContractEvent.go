package pubsub

import (
	"github.com/bloxapp/ssv/pubsub"
	"go.uber.org/zap"
)

type SmartContractEvent struct {
	pubsub.BaseObserver
}

func (s *SmartContractEvent) Update(vLog interface{}) {
	s.Logger.Debug("Got log from contract", zap.Any("log", vLog)) // pointer to Event log
}
