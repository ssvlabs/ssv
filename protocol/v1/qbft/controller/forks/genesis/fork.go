package genesis

import (
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	controcllerfork "github.com/bloxapp/ssv/protocol/v1/qbft/controller/forks"
	instancefork "github.com/bloxapp/ssv/protocol/v1/qbft/instance/forks"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/forks/genesis"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/changeround"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signedmsg"
)

// ForkGenesis is the genesis fork for controller
type ForkGenesis struct {
}

// New returns new ForkV0
func New() controcllerfork.Fork {
	return &ForkGenesis{}
}

// VersionName returns the name of the fork
func (g *ForkGenesis) VersionName() string {
	return "v1"
}

// InstanceFork returns instance fork
func (g *ForkGenesis) InstanceFork() instancefork.Fork {
	return genesis.New()
}

// ValidateDecidedMsg impl
func (g *ForkGenesis) ValidateDecidedMsg(share *beacon.Share) pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.MsgTypeCheck(message.CommitMsgType),
		signedmsg.AuthorizeMsg(share, g.VersionName()),
		signedmsg.ValidateQuorum(share.ThresholdSize()),
	)
}

// ValidateChangeRoundMsg impl
func (g *ForkGenesis) ValidateChangeRoundMsg(share *beacon.Share, identifier message.Identifier) pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.MsgTypeCheck(message.RoundChangeMsgType),
		signedmsg.ValidateLambdas(identifier),
		signedmsg.AuthorizeMsg(share, g.VersionName()),
		changeround.Validate(share),
	)
}

// Identifier return the proper identifier
func (g *ForkGenesis) Identifier(pk []byte, role message.RoleType) []byte {
	return message.NewIdentifier(pk, role)
}
