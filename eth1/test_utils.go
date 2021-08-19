package eth1

import (
	"github.com/bloxapp/ssv/pubsub"
	"math/big"
	"time"
)

// ClientMock implements eth1.Client interface
type ClientMock struct {
	Sub pubsub.Subject

	SyncTimeout  time.Duration
	SyncResponse error
}

// EventsSubject mocking subject
func (ec *ClientMock) EventsSubject() pubsub.Subscriber {
	return ec.Sub
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
