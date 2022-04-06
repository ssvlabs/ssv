package v0

import (
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/forks"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"
)

// ForkV0 is the genesis fork for instances
type ForkV0 struct {
	instance *instance.Instance
}

// New returns new ForkV0
func New() forks.Fork {
	return &ForkV0{}
}

// Apply - applies instance fork
func (v0 *ForkV0) Apply(instance *instance.Instance) {
	v0.instance = instance
}

// PrePrepareMsgPipeline - is the full processing msg pipeline for a pre-prepare msg
func (v0 *ForkV0) PrePrepareMsgPipeline() validation.SignedMessagePipeline {
	return v0.instance.PrePrepareMsgPipelineV0()
}

// PrepareMsgPipeline - is the full processing msg pipeline for a prepare msg
func (v0 *ForkV0) PrepareMsgPipeline() validation.SignedMessagePipeline {
	return v0.instance.PrepareMsgPipelineV0()
}

// CommitMsgValidationPipeline - is a msg validation ONLY pipeline
func (v0 *ForkV0) CommitMsgValidationPipeline() validation.SignedMessagePipeline {
	return v0.instance.CommitMsgValidationPipelineV0()
}

// CommitMsgPipeline - is the full processing msg pipeline for a commit msg
func (v0 *ForkV0) CommitMsgPipeline() validation.SignedMessagePipeline {
	return v0.instance.CommitMsgPipelineV0()
}

// DecidedMsgPipeline - is a specific full processing pipeline for a decided msg
func (v0 *ForkV0) DecidedMsgPipeline() validation.SignedMessagePipeline {
	return v0.instance.DecidedMsgPipelineV0()
}

// ChangeRoundMsgValidationPipeline - is a msg validation ONLY pipeline for a change round msg
func (v0 *ForkV0) ChangeRoundMsgValidationPipeline() validation.SignedMessagePipeline {
	return v0.instance.ChangeRoundMsgValidationPipelineV0()
}

// ChangeRoundMsgPipeline - is the full processing msg pipeline for a change round msg
func (v0 *ForkV0) ChangeRoundMsgPipeline() validation.SignedMessagePipeline {
	return v0.instance.ChangeRoundMsgPipelineV0()
}
