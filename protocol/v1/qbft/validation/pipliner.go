package validation

import (
	"github.com/bloxapp/ssv/ibft/pipeline"
)

// Pipelines holds all major instance pipeline implementations
type Pipelines interface {
	// PrePrepareMsgPipeline is the full processing msg pipeline for a pre-prepare msg
	PrePrepareMsgPipeline() pipeline.Pipeline
	// PrepareMsgPipeline is the full processing msg pipeline for a prepare msg
	PrepareMsgPipeline() pipeline.Pipeline
	// CommitMsgValidationPipeline is a msg validation ONLY pipeline
	CommitMsgValidationPipeline() pipeline.Pipeline
	// CommitMsgPipeline is the full processing msg pipeline for a commit msg
	CommitMsgPipeline() pipeline.Pipeline
	// DecidedMsgPipeline is a specific full processing pipeline for a decided msg
	DecidedMsgPipeline() pipeline.Pipeline
	// changeRoundMsgValidationPipeline is a msg validation ONLY pipeline for a change round msg
	ChangeRoundMsgValidationPipeline() pipeline.Pipeline
	// ChangeRoundMsgPipeline is the full processing msg pipeline for a change round msg
	ChangeRoundMsgPipeline() pipeline.Pipeline
}
