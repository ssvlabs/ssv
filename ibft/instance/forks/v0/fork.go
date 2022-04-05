package v0

import (
	ibftinstance "github.com/bloxapp/ssv/ibft/instance"
	"github.com/bloxapp/ssv/ibft/instance/forks"
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
)

// ForkV0 is the genesis fork for instances
type ForkV0 struct {
	instance *ibftinstance.Instance
}

// New returns new ForkV0
func New() forks.Fork {
	return &ForkV0{}
}

// Apply - applies instance fork
func (v0 *ForkV0) Apply(instance instance.Instance) {
	v0.instance = instance.(*ibftinstance.Instance)
}

// PrePrepareMsgPipeline - is the full processing msg pipeline for a pre-prepare msg
func (v0 *ForkV0) PrePrepareMsgPipeline() pipeline.Pipeline {
	return v0.instance.PrePrepareMsgPipelineV0()
}

// PrepareMsgPipeline - is the full processing msg pipeline for a prepare msg
func (v0 *ForkV0) PrepareMsgPipeline() pipeline.Pipeline {
	return v0.instance.PrepareMsgPipelineV0()
}

// CommitMsgValidationPipeline - is a msg validation ONLY pipeline
func (v0 *ForkV0) CommitMsgValidationPipeline() pipeline.Pipeline {
	return v0.instance.CommitMsgValidationPipelineV0()
}

// CommitMsgPipeline - is the full processing msg pipeline for a commit msg
func (v0 *ForkV0) CommitMsgPipeline() pipeline.Pipeline {
	return v0.instance.CommitMsgPipelineV0()
}

// DecidedMsgPipeline - is a specific full processing pipeline for a decided msg
func (v0 *ForkV0) DecidedMsgPipeline() pipeline.Pipeline {
	return v0.instance.DecidedMsgPipelineV0()
}

// ChangeRoundMsgValidationPipeline - is a msg validation ONLY pipeline for a change round msg
func (v0 *ForkV0) ChangeRoundMsgValidationPipeline() pipeline.Pipeline {
	return v0.instance.ChangeRoundMsgValidationPipelineV0()
}

// ChangeRoundMsgPipeline - is the full processing msg pipeline for a change round msg
func (v0 *ForkV0) ChangeRoundMsgPipeline() pipeline.Pipeline {
	return v0.instance.ChangeRoundMsgPipelineV0()
}
