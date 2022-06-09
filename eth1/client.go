package eth1

import (
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/prysmaticlabs/prysm/async/event"
)

//go:generate mockgen -package=eth1 -destination=./mock_client.go -source=./client.go

// Event represents an eth1 event log in the system
type Event struct {
	// Log is the raw event log
	Log types.Log
	// Name is the event name used for internal representation.
	Name string
	// Data is the parsed event
	Data interface{}
}

// SyncEndedEvent meant to notify an observer that the sync is over
type SyncEndedEvent struct {
	// Success returns true if the sync went well (all events were parsed)
	Success bool
	// Logs is the actual logs that we got from eth1
	Logs []types.Log
}

// Client represents the required interface for eth1 client
type Client interface {
	EventsFeed() *event.Feed
	Start() error
	Sync(fromBlock *big.Int) error
}
