package qbftstorage

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/protocol/v1/qbft"
)

// DecidedMsgStore manages persistence of messages
type DecidedMsgStore interface {
	GetLastDecided(identifier []byte) (*specqbft.SignedMessage, error)
	// SaveLastDecided saves the given decided message, after checking that it is indeed the highest
	SaveLastDecided(signedMsg ...*specqbft.SignedMessage) error
	// GetDecided returns historical decided messages in the given range
	GetDecided(identifier []byte, from specqbft.Height, to specqbft.Height) ([]*specqbft.SignedMessage, error)
	// SaveDecided saves historical decided messages
	SaveDecided(signedMsg ...*specqbft.SignedMessage) error
	// CleanAllDecided removes all decided & last decided for msgId
	CleanAllDecided(msgID []byte) error
}

// InstanceStore manages instance data
type InstanceStore interface {
	// SaveCurrentInstance saves the state for the current running (not yet decided) instance
	SaveCurrentInstance(identifier []byte, state *qbft.State) error
	// GetCurrentInstance returns the state for the current running (not yet decided) instance
	GetCurrentInstance(identifier []byte) (*qbft.State, bool, error)
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
