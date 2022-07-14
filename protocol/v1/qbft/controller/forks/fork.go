package forks

import (
	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/forks"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
)

// Fork holds all fork related implementations for the controller
type Fork interface {
	InstanceFork() forks.Fork
	ValidateDecidedMsg(share *beacon.Share) pipelines.SignedMessagePipeline
	ValidateChangeRoundMsg(share *beacon.Share, identifier message.Identifier) pipelines.SignedMessagePipeline
	VersionName() string
	Identifier(pk []byte, role spectypes.BeaconRole) []byte
}
