package storage

import (
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/bloxapp/ssv/pubsub"
)

// DBEvent struct
type DBEvent struct {
	pubsub.BaseSubject
	Log  types.Log
	Data interface{}
}

// NewDBEvent create new event subject
func NewDBEvent(name string) *DBEvent {
	return &DBEvent{
		BaseSubject: pubsub.BaseSubject{
			Name: name,
		},
	}
}

// NotifyAll notify all subscribe observables
func (e *DBEvent) NotifyAll() {
	for _, observer := range e.ObserverList {
		observer.InformObserver(e.Data)
	}
}
