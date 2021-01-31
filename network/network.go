package network

import (
	"github.com/bloxapp/ssv/ibft/proto"
)

type PipelineFunc func(signedMessage *proto.SignedMessage) error
type Pipeline []PipelineFunc

func (p Pipeline) Run(signedMessage *proto.SignedMessage) error {
	for _, pp := range p {
		if err := pp(signedMessage); err != nil {
			return err
		}
	}
	return nil
}

type Network interface {
	// Broadcast propagates a signed message to all peers
	Broadcast(msg *proto.SignedMessage) error

	// ReceivedMsgChan is a channel that forwards new propagated messages to a subscriber
	ReceivedMsgChan(id uint64) chan *proto.SignedMessage
}
