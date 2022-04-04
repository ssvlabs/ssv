package qbftstorage

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
)

type InstanceStore interface {
	// SaveCurrentInstance saves the state for the current running (not yet decided) instance
	SaveCurrentInstance(identifier message.Identifier, state *qbft.State) error
	// GetCurrentInstance returns the state for the current running (not yet decided) instance
	GetCurrentInstance(identifier message.Identifier) (*qbft.State, bool, error)
	// GetLastChangeRoundMsg returns the latest broadcasted msg from the instance
	GetLastChangeRoundMsg(identifier message.Identifier) (*message.SignedMessage, error)
}
