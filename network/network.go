package network

import (
	"github.com/bloxapp/ssv/ibft/proto"
)

type Network interface {
	// Broadcast propagates a signed message to all peers
	Broadcast(msg *proto.SignedMessage) error

	// ReceivedMsgChan is a channel that forwards new propagated messages to a subscriber
	ReceivedMsgChan(id uint64) <-chan *proto.SignedMessage
}
