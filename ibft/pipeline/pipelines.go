package pipeline

// Pipelines holds all major instance pipeline implementations
type Pipelines interface {
	// PrePrepareMsgPipeline is the full processing msg pipeline for a pre-prepare msg
	PrePrepareMsgPipeline() Pipeline
	// PrepareMsgPipeline is the full processing msg pipeline for a prepare msg
	PrepareMsgPipeline() Pipeline
	// CommitMsgValidationPipeline is a msg validation ONLY pipeline
	CommitMsgValidationPipeline() Pipeline
	// CommitMsgPipeline is the full processing msg pipeline for a commit msg
	CommitMsgPipeline() Pipeline
	// DecidedMsgPipeline is a specific full processing pipeline for a decided msg
	DecidedMsgPipeline() Pipeline
	// changeRoundMsgValidationPipeline is a msg validation ONLY pipeline for a change round msg
	ChangeRoundMsgValidationPipeline() Pipeline
	// ChangeRoundMsgPipeline is the full processing msg pipeline for a change round msg
	ChangeRoundMsgPipeline() Pipeline
}
