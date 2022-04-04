package qbftstorage

import "github.com/bloxapp/ssv/protocol/v1/message"

// DecidedMsgStore manages persistence of messages
type DecidedMsgStore interface {
	GetLastDecided(identifier message.Identifier)
	// SaveLastDecided saves the given decided message, after checking that it is indeed the highest
	SaveLastDecided(signedMsg ...*message.SignedMessage) error
	// GetDecided returns historical decided messages in the given range
	GetDecided(identifier message.Identifier, from message.Height, to message.Height) ([]*message.SignedMessage, error)
	// SaveDecided saves historical decided messages
	SaveDecided(signedMsg ...*message.SignedMessage) error
}
