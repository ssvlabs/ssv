package qbftstorage

import "github.com/bloxapp/ssv/protocol/v1/qbft"

type InstanceStore interface {
	// SaveCurrentInstance saves the state for the current running (not yet decided) instance
	SaveCurrentInstance(identifier []byte, state *qbft.State) error
	// GetCurrentInstance returns the state for the current running (not yet decided) instance
	GetCurrentInstance(identifier []byte) (*qbft.State, bool, error)
}
