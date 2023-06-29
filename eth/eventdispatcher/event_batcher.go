package eventdispatcher

import (
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/bloxapp/ssv/eth/eventbatcher"
)

type eventBatcher interface {
	BatchEvents(events <-chan ethtypes.Log) <-chan eventbatcher.BlockEvents
}
