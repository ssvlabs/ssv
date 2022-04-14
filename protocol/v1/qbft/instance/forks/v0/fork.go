package v0

import (
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/forks"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/changeround"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/preprepare"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/signedmsg"
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

// PrePrepareMsgValidationPipeline is the validation pipeline for pre-prepare messages
func (v0 *ForkV0) PrePrepareMsgValidationPipeline(share *beacon.Share, state *qbft.State, valCheck validation.ValueCheck,
	roundLeader preprepare.LeaderResolver) pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.MsgTypeCheck(message.ProposalMsgType),
		signedmsg.ValidateLambdas(state.GetIdentifier()),
		signedmsg.ValidateSequenceNumber(state.GetHeight()),
		signedmsg.AuthorizeMsg(share),
		preprepare.ValidatePrePrepareMsg(valCheck, roundLeader),
	)
}

// PrepareMsgValidationPipeline is the validation pipeline for prepare messages
func (v0 *ForkV0) PrepareMsgValidationPipeline(share *beacon.Share, state *qbft.State) pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.MsgTypeCheck(message.PrepareMsgType),
		signedmsg.ValidateLambdas(state.GetIdentifier()),
		signedmsg.ValidateSequenceNumber(state.GetHeight()),
		signedmsg.AuthorizeMsg(share),
	)
}

// CommitMsgValidationPipeline is the validation pipeline for commit messages
func (v0 *ForkV0) CommitMsgValidationPipeline(share *beacon.Share, identifier message.Identifier, height message.Height) pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.MsgTypeCheck(message.CommitMsgType),
		signedmsg.ValidateLambdas(identifier),
		signedmsg.ValidateSequenceNumber(height),
		signedmsg.AuthorizeMsg(share),
	)
}

// ChangeRoundMsgValidationPipeline is the validation pipeline for commit messages
func (v0 *ForkV0) ChangeRoundMsgValidationPipeline(share *beacon.Share, identifier message.Identifier, height message.Height) pipelines.SignedMessagePipeline {
	return pipelines.Combine(
		signedmsg.BasicMsgValidation(),
		signedmsg.MsgTypeCheck(message.RoundChangeMsgType),
		signedmsg.ValidateLambdas(identifier),
		signedmsg.ValidateSequenceNumber(height),
		signedmsg.AuthorizeMsg(share),
		changeround.Validate(share),
	)
}
