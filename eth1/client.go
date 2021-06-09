package eth1

import (
	"crypto/rsa"
	"github.com/bloxapp/ssv/pubsub"
	"github.com/ethereum/go-ethereum/core/types"
	"math/big"
)

// Event represents an eth1 event log in the system
type Event struct {
	Log  types.Log
	Data interface{}
}

// SyncEndedEvent meant to notify an observer that the sync is over
type SyncEndedEvent struct {
	// Success returns true if the sync went well (all events were parsed)
	Success bool
	// Logs is the actual logs that we got from eth1
	Logs []types.Log
}

// OperatorPrivateKeyProvider is a function that returns the operator private key
type OperatorPrivateKeyProvider = func() (*rsa.PrivateKey, error)

// Client represents the required interface for eth1 client
type Client interface {
	Subject() pubsub.Subscriber
	Start() error
	Sync(fromBlock *big.Int) error
}
