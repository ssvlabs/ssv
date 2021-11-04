package eth1

import (
	"github.com/bloxapp/ssv/pubsub"
	"math/big"
	"time"
)

// ClientMock implements eth1.Client interface
type ClientMock struct {
	Emitter pubsub.Emitter

	SyncTimeout  time.Duration
	SyncResponse error
}

// EventEmitter returns the contract events emitter
func (ec *ClientMock) EventEmitter() pubsub.EventSubscriber {
	return ec.Emitter
}

// Start mocking client init
func (ec *ClientMock) Start() error {
	return nil
}

// Sync mocking events sync
func (ec *ClientMock) Sync(fromBlock *big.Int) error {
	<-time.After(ec.SyncTimeout)
	return ec.SyncResponse
}
