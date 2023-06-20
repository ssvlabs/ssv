package eventbatcher

import (
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

type BlockEvents struct {
	BlockNumber uint64 // TODO: check if it's needed
	Events      []ethtypes.Log
}

// TODO: decide on its location.
func (be *BlockEvents) CreateTasks() []EventTask {
	// TODO: implement
	return nil
}
