package eth1

import (
	"github.com/prysmaticlabs/prysm/async/event"
	"math/big"
	"time"
)

// ClientMock implements eth1.Client interface
type ClientMock struct {
	Feed *event.Feed

	SyncTimeout  time.Duration
	SyncResponse error
}

// EventsFeed returns the contract events feed
func (ec *ClientMock) EventsFeed() *event.Feed {
	return ec.Feed
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

// CurrentBlock returns currentBlock
func (ec *ClientMock) CurrentBlock() (uint64, error) {
	return 0, nil
}
