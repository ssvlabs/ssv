package forks

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	"github.com/bloxapp/ssv/protocol/v1/qbft/validation/preprepare"
)

// Fork will apply fork modifications on an ibft instance
type Fork interface {
	msgValidation
	VersionName() string
}

type msgValidation interface {
	// PrePrepareMsgValidationPipeline is the validation pipeline for pre-prepare messages
	PrePrepareMsgValidationPipeline(share *beacon.Share, state *qbft.State, roundLeader preprepare.LeaderResolver) pipelines.SignedMessagePipeline
	// PrepareMsgValidationPipeline is the validation pipeline for prepare messages
	PrepareMsgValidationPipeline(share *beacon.Share, state *qbft.State) pipelines.SignedMessagePipeline
	// CommitMsgValidationPipeline is the validation pipeline for commit messages
	CommitMsgValidationPipeline(share *beacon.Share, state *qbft.State) pipelines.SignedMessagePipeline
	// ChangeRoundMsgValidationPipeline is the validation pipeline for commit messages
	ChangeRoundMsgValidationPipeline(share *beacon.Share, identifier []byte, height specqbft.Height) pipelines.SignedMessagePipeline
}
