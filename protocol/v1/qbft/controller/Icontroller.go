package controller

import (
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"go.uber.org/zap"
)

// IController represents behavior of the IController
type IController interface {
	// Init should be called after creating an IController instance to init the instance, sync it, etc.
	Init() error

	// StartInstance starts a new instance by the given options
	StartInstance(opts instance.ControllerStartInstanceOptions) (*instance.InstanceResult, error)

	// NextSeqNumber returns the previous decided instance seq number + 1
	// In case it's the first instance it returns 0
	NextSeqNumber() (message.Height, error)

	// GetIBFTCommittee returns a map of the iBFT committee where the key is the member's id.
	GetIBFTCommittee() map[beaconprotocol.OperatorID]*beaconprotocol.Node

	// GetIdentifier returns ibft identifier made of public key and role (type)
	GetIdentifier() []byte

	ProcessMsg(msg *message.SSVMessage) error

	// ProcessSignatureMessage aggregate signature messages and broadcasting when quorum achieved
	ProcessSignatureMessage(msg *message.SignedPostConsensusMessage) error

	// PostConsensusDutyExecution signs the eth2 duty after iBFT came to consensus and start signature state
	PostConsensusDutyExecution(logger *zap.Logger, height message.Height, decidedValue []byte, signaturesCount int, duty *beaconprotocol.Duty) error
}
