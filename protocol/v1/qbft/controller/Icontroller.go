package controller

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
)

// IController represents behavior of the IController
type IController interface {
	// Init should be called after creating an IController instance to init the instance, sync it, etc.
	Init() error

	// StartInstance starts a new instance by the given options
	StartInstance(opts instance.ControllerStartInstanceOptions) (*instance.Result, error)

	// NextSeqNumber returns the previous decided instance seq number + 1
	// In case it's the first instance it returns 0
	NextSeqNumber() (specqbft.Height, error)

	// GetIBFTCommittee returns a map of the iBFT committee where the key is the member's id.
	GetIBFTCommittee() map[spectypes.OperatorID]*beaconprotocol.Node

	// GetIdentifier returns ibft identifier made of public key and role (type)
	GetIdentifier() []byte

	ProcessMsg(msg *spectypes.SSVMessage) error

	// ProcessPostConsensusMessage aggregates partial signature messages and broadcasting when quorum achieved
	ProcessPostConsensusMessage(msg *ssv.SignedPartialSignatureMessage) error

	// PostConsensusDutyExecution signs the eth2 duty after iBFT came to consensus and start signature state
	PostConsensusDutyExecution(logger *zap.Logger, height specqbft.Height, decidedValue []byte, signaturesCount int, duty *spectypes.Duty) error

	// OnFork called when fork occur.
	OnFork(forkVersion forksprotocol.ForkVersion) error

	// GetCurrentInstance returns current instance if exist. if not, returns nil TODO for mapping, need to remove once duty runner implemented
	GetCurrentInstance() instance.Instancer
}
