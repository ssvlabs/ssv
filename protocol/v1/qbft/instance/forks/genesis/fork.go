package genesis

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"

	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/forks"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/changeround"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/preprepare"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signedmsg"
)

// ForkGenesis is the genesis fork for instances
type ForkGenesis struct {
	instance *instance.Instance
}

// New returns new ForkGenesis
func New() forks.Fork {
	return &ForkGenesis{}
}

// Apply - applies instance fork
func (g *ForkGenesis) Apply(instance *instance.Instance) {
	g.instance = instance
}

// VersionName returns version name
func (g *ForkGenesis) VersionName() string {
	return forksprotocol.GenesisForkVersion.String()
}

// PrePrepareMsgValidationPipeline is the validation pipeline for pre-prepare messages
func (g *ForkGenesis) PrePrepareMsgValidationPipeline(share *beacon.Share, state *qbft.State, roundLeader preprepare.LeaderResolver) pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.MsgTypeCheck(specqbft.ProposalMsgType),
		signedmsg.ValidateLambdas(state.GetIdentifier()),
		signedmsg.ValidateSequenceNumber(state.GetHeight()),
		signedmsg.AuthorizeMsg(share),
		preprepare.ValidatePrePrepareMsg(roundLeader),
	)
}

// PrepareMsgValidationPipeline is the validation pipeline for prepare messages
func (g *ForkGenesis) PrepareMsgValidationPipeline(share *beacon.Share, state *qbft.State) pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.MsgTypeCheck(specqbft.PrepareMsgType),
		signedmsg.ValidateLambdas(state.GetIdentifier()),
		signedmsg.ValidateSequenceNumber(state.GetHeight()),
		signedmsg.AuthorizeMsg(share),
	)
}

// CommitMsgValidationPipeline is the validation pipeline for commit messages
func (g *ForkGenesis) CommitMsgValidationPipeline(share *beacon.Share, identifier message.Identifier, height specqbft.Height) pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.MsgTypeCheck(specqbft.CommitMsgType),
		signedmsg.ValidateLambdas(identifier),
		signedmsg.ValidateSequenceNumber(height),
		signedmsg.AuthorizeMsg(share),
	)
}

// ChangeRoundMsgValidationPipeline is the validation pipeline for commit messages
func (g *ForkGenesis) ChangeRoundMsgValidationPipeline(share *beacon.Share, identifier message.Identifier, height specqbft.Height) pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.MsgTypeCheck(specqbft.RoundChangeMsgType),
		signedmsg.ValidateLambdas(identifier),
		signedmsg.ValidateSequenceNumber(height),
		signedmsg.AuthorizeMsg(share),
		changeround.Validate(share),
	)
}
