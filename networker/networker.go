package networker

import "github.com/bloxapp/ssv/ibft/types"

type PipelineFunc func(signedMessage *types.SignedMessage) error

type Networker interface {
	// Broadcast propagates a signed message to all peers
	Broadcast(msg *types.SignedMessage) error

	// SetMessagePipeline sets a pipeline for a message to go through before it's sent to the msg channel.
	// Message validation and processing should happen in the pipeline
	SetMessagePipeline(id uint64, roundState types.RoundState, pipeline []PipelineFunc)
}
