package v0

import (
	"github.com/bloxapp/ssv/protocol/v1/keymanager"
	"github.com/bloxapp/ssv/protocol/v1/message"
	controcllerfork "github.com/bloxapp/ssv/protocol/v1/qbft/controller/forks"
	instancefork "github.com/bloxapp/ssv/protocol/v1/qbft/instance/forks"
	forkv0 "github.com/bloxapp/ssv/protocol/v1/qbft/instance/forks/v0"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signed_msg"
)

// ForkV0 is the genesis fork for controller
type ForkV0 struct {
}

// New returns new ForkV0
func New() controcllerfork.Fork {
	return &ForkV0{}
}

// InstanceFork returns instance fork
func (v0 *ForkV0) InstanceFork() instancefork.Fork {
	return forkv0.New()
}

// ValidateDecidedMsg impl
func (v0 *ForkV0) ValidateDecidedMsg(share *keymanager.Share) validation.SignedMessagePipeline {
	return validation.Combine(
		signed_msg.BasicMsgValidation(),
		signed_msg.MsgTypeCheck(message.CommitMsgType),
		signed_msg.AuthorizeMsg(share),
		signed_msg.ValidateQuorum(share.ThresholdSize()),
	)
}
