package protocolp2p

import (
	"errors"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv-spec/p2p"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var (
	// ErrNetworkIsNotReady is returned when trying to access the network instance before it's ready
	ErrNetworkIsNotReady = errors.New("network services are not ready")
)

// Subscriber manages topics subscription
type Subscriber interface {
	p2p.Subscriber
	// Unsubscribe unsubscribes from the validator subnet
	Unsubscribe(pk spectypes.ValidatorPK) error
	// Peers returns the peers that are connected to the given validator
}

// Broadcaster enables to broadcast messages
type Broadcaster interface {
	Broadcast(id spectypes.MessageID, message *spectypes.SignedSSVMessage) error
	// BroadcastAtSlot behaves like Broadcast but takes the message's slot explicitly, which
	// determines whether it's sent on Alan or Boole topics (see networkconfig.Network.BooleForkAtSlot).
	BroadcastAtSlot(message *spectypes.SignedSSVMessage, slot phase0.Slot) error
}

// Network holds the networking layer used to complement the underlying protocols
type Network interface {
	Subscriber
	Broadcaster
}
