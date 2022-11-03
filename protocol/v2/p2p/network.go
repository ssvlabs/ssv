package protocolp2p

import (
	"github.com/bloxapp/ssv-spec/p2p"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/libp2p/go-libp2p-core/peer"
)

// Subscriber manages topics subscription
type Subscriber interface {
	p2p.Subscriber
	// Unsubscribe unsubscribes from the validator subnet
	Unsubscribe(pk spectypes.ValidatorPK) error
	// Peers returns the peers that are connected to the given validator
	Peers(pk spectypes.ValidatorPK) ([]peer.ID, error)
}

// Broadcaster enables to broadcast messages
type Broadcaster interface {
	p2p.Broadcaster
}

// Syncer holds the interface for syncing data from other peers
type Syncer interface {
	specqbft.Syncer
	// GetHistory sync the given range from a set of peers that supports history for the given identifier
	// it accepts a list of targets for the request.
	//GetHistory(mid spectypes.MessageID, from, to specqbft.Height, targets ...string) ([]SyncResult, specqbft.Height, error)

	// RegisterHandlers registers handler for the given protocol
	//RegisterHandlers(handlers ...*SyncHandler)
}

// Network holds the networking layer used to complement the underlying protocols
type Network interface {
	Subscriber
	Broadcaster
	Syncer
}
