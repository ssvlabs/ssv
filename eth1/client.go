package eth1

import (
	"crypto/rsa"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/prysmaticlabs/prysm/async/event"
	"math/big"
)

// Event represents an eth1 event log in the system
type Event struct {
	// Log is the raw event log
	Log types.Log
	// Data is the parsed event
	Data interface{}
	// IsOperatorEvent indicates whether the event belongs to operator
	IsOperatorEvent bool
}

// SyncEndedEvent meant to notify an observer that the sync is over
type SyncEndedEvent struct {
	// Success returns true if the sync went well (all events were parsed)
	Success bool
	// Logs is the actual logs that we got from eth1
	Logs []types.Log
}

// ShareEncryptionKeyProvider is a function that returns the operator private key
type ShareEncryptionKeyProvider = func() (*rsa.PrivateKey, bool, error)

// Client represents the required interface for eth1 client
type Client interface {
	EventsFeed() *event.Feed
	Start() error
	Sync(fromBlock *big.Int) error
}
