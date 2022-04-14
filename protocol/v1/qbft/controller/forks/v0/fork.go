package v0

import (
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	controcllerfork "github.com/bloxapp/ssv/protocol/v1/qbft/controller/forks"
	instancefork "github.com/bloxapp/ssv/protocol/v1/qbft/instance/forks"
	forkv0 "github.com/bloxapp/ssv/protocol/v1/qbft/instance/forks/v0"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signedmsg"
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
func (v0 *ForkV0) ValidateDecidedMsg(share *beacon.Share) pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.MsgTypeCheck(message.CommitMsgType),
		signedmsg.AuthorizeMsg(share),
		signedmsg.ValidateQuorum(share.ThresholdSize()),
	)
}
