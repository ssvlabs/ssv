package eventbatcher

import (
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

type BlockEvents struct {
	BlockNumber uint64
	Events      []ethtypes.Log
}
