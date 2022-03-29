package p2p

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
)

// Subscriber manages topics subscription
type Subscriber interface {
	// Subscribe subscribes to validator subnet
	Subscribe(pk message.ValidatorPK) error
	// Unsubscribe unsubscribes from the validator subnet
	Unsubscribe(pk message.ValidatorPK) error
}

// Broadcaster enables to broadcast messages
type Broadcaster interface {
	// Broadcast broadcasts the given message to the corresponding subnet
	Broadcast(msg message.SSVMessage) error
}

// RequestHandler handles p2p requests
type RequestHandler func(*message.SSVMessage) (*message.SSVMessage, error)

// Syncer holds the interface for syncing data from other peerz
type Syncer interface {
	// RegisterHandler registers handler for the given protocol
	RegisterHandler(protocol string, handler RequestHandler)
	// LastDecided fetches last decided from a random set of peers
	LastDecided(mid message.Identifier) ([]message.SSVMessage, error)
	// GetHistory sync the given range from a set of peers that supports history for the given identifier
	GetHistory(mid message.Identifier, from, to uint64) ([]message.SSVMessage, error)
	// LastChangeRound fetches last change round message from a random set of peers
	LastChangeRound(mid message.Identifier) ([]message.SSVMessage, error)
}

// Network holds the networking layer used to complement the underlying protocols
type Network interface {
	Subscriber
	Broadcaster
	Syncer
}
