package validation

import "github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"

// Pipelines holds all major instance pipeline implementations
type Pipelines interface {
	// PrePrepareMsgPipeline is the full processing msg pipeline for a pre-prepare msg
	PrePrepareMsgPipeline() pipelines.SignedMessagePipeline
	// PrepareMsgPipeline is the full processing msg pipeline for a prepare msg
	PrepareMsgPipeline() pipelines.SignedMessagePipeline
	// CommitMsgValidationPipeline is a msg validation ONLY pipeline
	CommitMsgValidationPipeline() pipelines.SignedMessagePipeline
	// CommitMsgPipeline is the full processing msg pipeline for a commit msg
	CommitMsgPipeline() pipelines.SignedMessagePipeline
	// DecidedMsgPipeline is a specific full processing pipeline for a decided msg
	DecidedMsgPipeline() pipelines.SignedMessagePipeline
	// ChangeRoundMsgValidationPipeline is a msg validation ONLY pipeline for a change round msg
	ChangeRoundMsgValidationPipeline() pipelines.SignedMessagePipeline
	// ChangeRoundMsgPipeline is the full processing msg pipeline for a change round msg
	ChangeRoundMsgPipeline() pipelines.SignedMessagePipeline
}
