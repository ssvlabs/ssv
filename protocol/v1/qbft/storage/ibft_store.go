package qbftstorage

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
)

// DecidedMsgStore manages persistence of messages
type DecidedMsgStore interface {
	GetLastDecided(identifier message.Identifier) (*specqbft.SignedMessage, error)
	// SaveLastDecided saves the given decided message, after checking that it is indeed the highest
	SaveLastDecided(signedMsg ...*specqbft.SignedMessage) error
	// GetDecided returns historical decided messages in the given range
	GetDecided(identifier message.Identifier, from specqbft.Height, to specqbft.Height) ([]*specqbft.SignedMessage, error)
	// SaveDecided saves historical decided messages
	SaveDecided(signedMsg ...*specqbft.SignedMessage) error
}

// InstanceStore manages instance data
type InstanceStore interface {
	// SaveCurrentInstance saves the state for the current running (not yet decided) instance
	SaveCurrentInstance(identifier message.Identifier, state *qbft.State) error
	// GetCurrentInstance returns the state for the current running (not yet decided) instance
	GetCurrentInstance(identifier message.Identifier) (*qbft.State, bool, error)
}

// ChangeRoundStore manages change round data
type ChangeRoundStore interface {
	// GetLastChangeRoundMsg returns the latest broadcasted msg from the instance
	GetLastChangeRoundMsg(identifier message.Identifier, signers ...spectypes.OperatorID) ([]*specqbft.SignedMessage, error)
	// SaveLastChangeRoundMsg returns the latest broadcasted msg from the instance
	SaveLastChangeRoundMsg(msg *specqbft.SignedMessage) error
	// CleanLastChangeRound cleans last change round message of some validator, should be called upon controller init
	CleanLastChangeRound(identifier message.Identifier)
}

// QBFTStore is the store used by QBFT components
type QBFTStore interface {
	DecidedMsgStore
	InstanceStore
	ChangeRoundStore
}
