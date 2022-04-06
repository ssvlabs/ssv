package qbftstorage

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
)

// DecidedMsgStore manages persistence of messages
type DecidedMsgStore interface {
	GetLastDecided(identifier message.Identifier) (*message.SignedMessage, error)
	// SaveLastDecided saves the given decided message, after checking that it is indeed the highest
	SaveLastDecided(signedMsg ...*message.SignedMessage) error
	// GetDecided returns historical decided messages in the given range
	GetDecided(identifier message.Identifier, from message.Height, to message.Height) ([]*message.SignedMessage, error)
	// SaveDecided saves historical decided messages
	SaveDecided(signedMsg ...*message.SignedMessage) error
}

// InstanceStore manages instance data
type InstanceStore interface {
	// SaveCurrentInstance saves the state for the current running (not yet decided) instance
	SaveCurrentInstance(identifier message.Identifier, state *qbft.State) error
	// GetCurrentInstance returns the state for the current running (not yet decided) instance
	GetCurrentInstance(identifier message.Identifier) (*qbft.State, bool, error)
	// GetLastChangeRoundMsg returns the latest broadcasted msg from the instance
	GetLastChangeRoundMsg(identifier message.Identifier) (*message.SignedMessage, error)
}

// QBFTStore is the store used by QBFT components
type QBFTStore interface {
	DecidedMsgStore
	InstanceStore
}
