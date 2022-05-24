package v1

import (
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	controcllerfork "github.com/bloxapp/ssv/protocol/v1/qbft/controller/forks"
	instancefork "github.com/bloxapp/ssv/protocol/v1/qbft/instance/forks"
	forkV1 "github.com/bloxapp/ssv/protocol/v1/qbft/instance/forks/v1"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signedmsg"
)

// ForkV1 is the genesis fork for controller
type ForkV1 struct {
}

// New returns new ForkV0
func New() controcllerfork.Fork {
	return &ForkV1{}
}

// VersionName returns the name of the fork
func (v1 *ForkV1) VersionName() string {
	return "v1"
}

// InstanceFork returns instance fork
func (v1 *ForkV1) InstanceFork() instancefork.Fork {
	return forkV1.New()
}

// ValidateDecidedMsg impl
func (v1 *ForkV1) ValidateDecidedMsg(share *beacon.Share) pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.MsgTypeCheck(message.CommitMsgType),
		signedmsg.AuthorizeMsg(share, v1.VersionName()),
		signedmsg.ValidateQuorum(share.ThresholdSize()),
	)
}

// Identifier return the proper identifier
func (v1 *ForkV1) Identifier(pk []byte, role message.RoleType) []byte {
	return message.NewIdentifier(pk, role)
}
