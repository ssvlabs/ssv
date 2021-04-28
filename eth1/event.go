package eth1

import (
	"github.com/bloxapp/ssv/pubsub"
	"github.com/ethereum/go-ethereum/core/types"
)

type Event struct {
	pubsub.BaseSubject
	//ObserverList []node.SmartContractEvent
	Log          types.Log
}

func NewEvent(name string) *Event {
	return &Event{
		BaseSubject: pubsub.BaseSubject{
			Name: name,
		},
	}
}

func (e *Event) NotifyAll(){
	for _, observer := range e.ObserverList {
		observer.Update(e.Log)
	}
}


