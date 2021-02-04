package network

import (
	"github.com/bloxapp/ssv/ibft/proto"
)

type Network interface {
	// Broadcast propagates a signed message to all peers
	Broadcast(lambda []byte, msg *proto.SignedMessage) error

	// ReceivedMsgChan is a channel that forwards new propagated messages to a subscriber
	// TODO: Get rid of identifier
	ReceivedMsgChan(id uint64, lambda []byte) <-chan *proto.SignedMessage
}
