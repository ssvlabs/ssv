package v0

import (
	ibft2 "github.com/bloxapp/ssv/ibft"
	ibft "github.com/bloxapp/ssv/ibft/instance"
	"github.com/bloxapp/ssv/ibft/pipeline"
)

type ForkV0 struct {
	instance *ibft.Instance
}

func New() *ForkV0 {
	return &ForkV0{}
}

func (v0 *ForkV0) Apply(instance ibft2.Instance) {
	v0.instance = instance.(*ibft.Instance)
}

// PrePrepareMsgPipelineV0 is the full processing msg pipeline for a pre-prepare msg
func (v0 *ForkV0) PrePrepareMsgPipeline() pipeline.Pipeline {
	return v0.instance.PrePrepareMsgPipelineV0()
}

// PrepareMsgPipeline is the full processing msg pipeline for a prepare msg
func (v0 *ForkV0) PrepareMsgPipeline() pipeline.Pipeline {
	return v0.instance.PrepareMsgPipelineV0()
}

// CommitMsgValidationPipeline is a msg validation ONLY pipeline
func (v0 *ForkV0) CommitMsgValidationPipeline() pipeline.Pipeline {
	return v0.instance.CommitMsgValidationPipelineV0()
}

// CommitMsgPipeline is the full processing msg pipeline for a commit msg
func (v0 *ForkV0) CommitMsgPipeline() pipeline.Pipeline {
	return v0.instance.CommitMsgPipelineV0()
}

// DecidedMsgPipeline is a specific full processing pipeline for a decided msg
func (v0 *ForkV0) DecidedMsgPipeline() pipeline.Pipeline {
	return v0.instance.DecidedMsgPipelineV0()
}

// changeRoundMsgValidationPipeline is a msg validation ONLY pipeline for a change round msg
func (v0 *ForkV0) ChangeRoundMsgValidationPipeline() pipeline.Pipeline {
	return v0.instance.ChangeRoundMsgValidationPipelineV0()
}

// ChangeRoundMsgPipeline is the full processing msg pipeline for a change round msg
func (v0 *ForkV0) ChangeRoundMsgPipeline() pipeline.Pipeline {
	return v0.instance.ChangeRoundMsgPipelineV0()
}
