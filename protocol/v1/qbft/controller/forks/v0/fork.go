package v0

import (
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	controllerfork "github.com/bloxapp/ssv/protocol/v1/qbft/controller/forks"
	instancefork "github.com/bloxapp/ssv/protocol/v1/qbft/instance/forks"
	forkv0 "github.com/bloxapp/ssv/protocol/v1/qbft/instance/forks/v0"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signedmsg"
	"github.com/bloxapp/ssv/utils/format"
)

// ForkV0 is the genesis fork for controller
type ForkV0 struct {
}

// New returns new ForkV0
func New() controllerfork.Fork {
	return &ForkV0{}
}

// VersionName returns the name of the fork
func (v0 *ForkV0) VersionName() string {
	return "v0"
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
		signedmsg.AuthorizeMsg(share, v0.VersionName()),
		signedmsg.ValidateQuorum(share.ThresholdSize()),
	)
}

// Identifier return the proper identifier
func (v0 *ForkV0) Identifier(pk []byte, role message.RoleType) []byte {
	return []byte(format.IdentifierFormat(pk, role.String())) // need to support same seed as v0 versions
}
