package controller

import (
	"github.com/bloxapp/ssv/protocol/v1/keymanager"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
)

// IController represents behavior of the IController
type IController interface {
	// Init should be called after creating an IController instance to init the instance, sync it, etc.
	Init() error

	// StartInstance starts a new instance by the given options
	StartInstance(opts instance.ControllerStartInstanceOptions) (*instance.InstanceResult, error)

	// NextSeqNumber returns the previous decided instance seq number + 1
	// In case it's the first instance it returns 0
	NextSeqNumber() (uint64, error)

	// GetIBFTCommittee returns a map of the iBFT committee where the key is the member's id.
	GetIBFTCommittee() map[keymanager.OperatorID]*keymanager.Node

	// GetIdentifier returns ibft identifier made of public key and role (type)
	GetIdentifier() []byte

	ProcessMsg(msg *message.SignedMessage)
}
