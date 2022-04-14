package validation

// Pipelines holds all major instance pipeline implementations
type Pipelines interface {
	// PrePrepareMsgPipeline is the full processing msg pipeline for a pre-prepare msg
	PrePrepareMsgPipeline() SignedMessagePipeline
	// PrepareMsgPipeline is the full processing msg pipeline for a prepare msg
	PrepareMsgPipeline() SignedMessagePipeline
	// CommitMsgValidationPipeline is a msg validation ONLY pipeline
	CommitMsgValidationPipeline() SignedMessagePipeline
	// CommitMsgPipeline is the full processing msg pipeline for a commit msg
	CommitMsgPipeline() SignedMessagePipeline
	// DecidedMsgPipeline is a specific full processing pipeline for a decided msg
	DecidedMsgPipeline() SignedMessagePipeline
	// ChangeRoundMsgValidationPipeline is a msg validation ONLY pipeline for a change round msg
	ChangeRoundMsgValidationPipeline() SignedMessagePipeline
	// ChangeRoundMsgPipeline is the full processing msg pipeline for a change round msg
	ChangeRoundMsgPipeline() SignedMessagePipeline
}
