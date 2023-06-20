package eventdispatcher

import (
	"github.com/bloxapp/ssv/eth1_refactor/eventbatcher"
)

type eventDataHandler interface {
	HandleBlockEventsStream(blockEvents <-chan eventbatcher.BlockEvents) error
}
