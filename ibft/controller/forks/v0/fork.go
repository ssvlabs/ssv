package v0

import (
	"github.com/bloxapp/ssv/ibft/controller/forks"
	instanceFork "github.com/bloxapp/ssv/ibft/instance/forks"
	instanceV0Fork "github.com/bloxapp/ssv/ibft/instance/forks/v0"
	"github.com/bloxapp/ssv/ibft/pipeline"
	"github.com/bloxapp/ssv/ibft/pipeline/auth"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/validator/storage"
)

// ForkV0 is the genesis fork for controller
type ForkV0 struct {
}

// New returns new ForkV0
func New() forks.Fork {
	return &ForkV0{}
}

// InstanceFork returns instance fork
func (v0 *ForkV0) InstanceFork() instanceFork.Fork {
	return instanceV0Fork.New()
}

// ValidateDecidedMsg impl
func (v0 *ForkV0) ValidateDecidedMsg(share *storage.Share) pipeline.Pipeline {
	return pipeline.Combine(
		auth.BasicMsgValidation(),
		auth.MsgTypeCheck(proto.RoundState_Commit),
		auth.AuthorizeMsg(share),
		auth.ValidateQuorum(share.ThresholdSize()),
	)
}
