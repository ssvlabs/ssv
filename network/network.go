package network

import (
	"github.com/bloxapp/ssv/ibft/proto"
)

// Network represents the behavior of the network
type Network interface {
	// Broadcast propagates a signed message to all peers
	Broadcast(lambda []byte, msg *proto.SignedMessage) error

	// ReceivedMsgChan is a channel that forwards new propagated messages to a subscriber
	// TODO: Get rid of identifier
	ReceivedMsgChan(id uint64, lambda []byte) <-chan *proto.SignedMessage

	// BroadcastSignature broadcasts the given signature for the given lambda
	BroadcastSignature(lambda []byte, signature map[uint64][]byte) error

	// ReceivedSignatureChan returns the channel with signatures
	// TODO: Get rid of identifier
	ReceivedSignatureChan(id uint64, lambda []byte) <-chan map[uint64][]byte
}
