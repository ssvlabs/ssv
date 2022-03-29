package v1

import "github.com/bloxapp/ssv/protocol/v1/message"

// MsgStore manages persistence of messages
type MsgStore interface {
	// SaveHighestDecided saves (and potentially overrides) the highest Decided for a specific instance
	SaveHighestDecided(signedMsg ...*message.SignedMessage) error
	// GetDecided returns decided messages in the given range
	GetDecided(identifier []byte, from uint64, to uint64) ([]message.SignedMessage, error)
	// SaveDecided returns decided messages in the given range
	SaveDecided(signedMsg ...*message.SignedMessage) error
}
