package types

type PipelineFunc func(signedMessage *SignedMessage) error

type Networker interface {
	// Broadcast propagates a signed message to all peers
	Broadcast(msg *SignedMessage) error

	// SetMessagePipeline sets a pipeline for a message to go through before it's sent to the msg channel.
	// Message validation and processing should happen in the pipeline
	SetMessagePipeline(id uint64, roundState RoundState, pipeline []PipelineFunc)
}
