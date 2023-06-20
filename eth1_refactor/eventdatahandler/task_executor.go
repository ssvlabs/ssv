package eventdatahandler

import (
	"github.com/bloxapp/ssv/eth1_refactor/eventbatcher"
)

type taskExecutor interface {
	ExecuteTasks(tasks ...eventbatcher.EventTask) error
}
