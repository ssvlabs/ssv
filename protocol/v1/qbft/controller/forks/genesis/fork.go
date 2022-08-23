package genesis

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
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
		signedmsg.MsgTypeCheck(specqbft.CommitMsgType),
		signedmsg.AuthorizeMsg(share),
		signedmsg.ValidateQuorum(share.ThresholdSize()),
	)
}

// ValidateChangeRoundMsg impl
func (g *ForkGenesis) ValidateChangeRoundMsg(share *beacon.Share, identifier spectypes.MessageID) pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.MsgTypeCheck(specqbft.RoundChangeMsgType),
		signedmsg.ValidateIdentifiers(identifier[:]),
		signedmsg.AuthorizeMsg(share),
		changeround.Validate(share),
	)
}

// Identifier return the proper identifier
func (g *ForkGenesis) Identifier(pk []byte, role spectypes.BeaconRole) []byte {
	id := spectypes.NewMsgID(pk, role)
	return id[:]
}
