package network

import (
	"github.com/bloxapp/ssv/ibft/proto"
)

type PipelineFunc func(signedMessage *proto.SignedMessage) error

type Network interface {
	// Broadcast propagates a signed message to all peers
	Broadcast(msg *proto.SignedMessage) error

	// SetMessagePipeline sets a pipeline for a message to go through before it's sent to the msg channel.
	// Message validation and processing should happen in the pipeline
	SetMessagePipeline(id uint64, roundState proto.RoundState, pipeline []PipelineFunc)
}
