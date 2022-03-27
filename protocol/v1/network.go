package v1

import "github.com/bloxapp/ssv/protocol/v1/core"

// Broadcaster enables to broadcast messages
// TODO: decide whether to keep BroadcastDecided or re-use Broadcast
type Broadcaster interface {
	// Broadcast broadcasts the given message to the corresponding subnet
	Broadcast(msg core.SSVMessage) error
	// BroadcastDecided broadcasts the given decided message
	BroadcastDecided(msg core.SSVMessage) error
}

// RequestHandler handles stream requests
type RequestHandler func(*core.SSVMessage) (*core.SSVMessage, error)

// Syncer holds the interface for syncing data from other peerz
// TODO: decide whether to use an explicit api (LastDecided, GetHistory and LastChangeRound)
// 		or a single function (Request) to rule them all
type Syncer interface {
	// Handle registers handler for the given protocol
	Handle(protocol string, handler RequestHandler) error
	// Request perform a p2p request to peers in the network
	Request(protocol string, msg core.SSVMessage) ([]core.SSVMessage, error)
	//// LastDecided fetches last decided from a random set of peers
	//LastDecided(mid Identifier) ([]SSVMessage, error)
	//// GetHistory sync the given range from a set of peers that supports history for the given identifier
	//GetHistory(mid Identifier, from, to uint64) ([]SSVMessage, error)
	//// LastChangeRound fetches last change round message from a random set of peers
	//LastChangeRound(mid Identifier) ([]SSVMessage, error)
}

// Network holds the networking layer used to complement the underlying protocols
type Network interface {
	Broadcaster
	Syncer
}
