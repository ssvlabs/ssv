package eth1

import (
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/bloxapp/ssv/pubsub"
)

// ContractEvent struct
type ContractEvent struct {
	pubsub.BaseSubject
	Log types.Log
}

// NewContractEvent create new event subject
func NewContractEvent(name string) *ContractEvent {
	return &ContractEvent{
		BaseSubject: pubsub.BaseSubject{
			Name: name,
		},
	}
}

// NotifyAll notify all subscribe observables
func (e *ContractEvent) NotifyAll() {
	for _, observer := range e.ObserverList {
		observer.Update(e.Log)
	}
}
