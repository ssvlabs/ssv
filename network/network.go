package network

import (
	"github.com/bloxapp/ssv/ibft/proto"
)

// Network represents the behavior of the network
type Network interface {
	// Broadcast propagates a signed message to all peers
	Broadcast(msg *proto.SignedMessage) error

	// ReceivedMsgChan is a channel that forwards new propagated messages to a subscriber
	ReceivedMsgChan() <-chan *proto.SignedMessage

	// BroadcastSignature broadcasts the given signature for the given lambda
	BroadcastSignature(lambda []byte, signature map[uint64][]byte) error

	// ReceivedSignatureChan returns the channel with signatures
	ReceivedSignatureChan() <-chan map[uint64][]byte
}
