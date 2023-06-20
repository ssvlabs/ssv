package eventdispatcher

import (
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/bloxapp/ssv/eth1_refactor/eventbatcher"
)

type eventBatcher interface {
	BatchHistoricalEvents(events []ethtypes.Log) <-chan eventbatcher.BlockEvents
	BatchOngoingEvents(events <-chan ethtypes.Log) <-chan eventbatcher.BlockEvents
}
