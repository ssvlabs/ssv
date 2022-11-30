package qbftstorage

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

// DecidedMsgStore manages persistence of messages
type DecidedMsgStore interface {
	GetHighestDecided(identifier []byte) (*specqbft.SignedMessage, error)
	// SaveHighestDecided saves the given decided message, after checking that it is indeed the highest
	SaveHighestDecided(signedMsg ...*specqbft.SignedMessage) error
	// GetDecided returns historical decided messages in the given range
	GetDecided(identifier []byte, from specqbft.Height, to specqbft.Height) ([]*specqbft.SignedMessage, error)
	// SaveDecided saves historical decided messages
	SaveDecided(signedMsg ...*specqbft.SignedMessage) error
	// CleanAllDecided removes all decided & last decided for msgId
	CleanAllDecided(msgID []byte) error
}

// InstanceStore manages instance data
type InstanceStore interface {
	// SaveHighestInstance saves the state for the highest instance
	SaveHighestInstance(state *specqbft.State) error
	// GetHighestInstance returns the state for the highest instance
	GetHighestInstance(identifier []byte) (*specqbft.State, error)
	// GetInstance returns historical decided messages in the given range
	GetInstance(identifier []byte, from specqbft.Height, to specqbft.Height) ([]*specqbft.State, error)
	// SaveInstance saves historical decided messages
	SaveInstance(state *specqbft.State) error
}

// ChangeRoundStore manages change round data
type ChangeRoundStore interface {
	// GetLastChangeRoundMsg returns the latest broadcasted msg from the instance
	GetLastChangeRoundMsg(identifier []byte, signers ...spectypes.OperatorID) ([]*specqbft.SignedMessage, error)
	// SaveLastChangeRoundMsg returns the latest broadcasted msg from the instance
	SaveLastChangeRoundMsg(msg *specqbft.SignedMessage) error
	// CleanLastChangeRound cleans last change round message of some validator, should be called upon controller init
	CleanLastChangeRound(identifier []byte) error
}

// QBFTStore is the store used by QBFT components
type QBFTStore interface {
	DecidedMsgStore
	InstanceStore
	ChangeRoundStore
}
