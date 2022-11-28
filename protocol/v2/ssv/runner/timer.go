package runner

import (
	"fmt"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer"
)

func (b *BaseRunner) registerTimeoutHandler(instance *instance.Instance, height specqbft.Height) {
	timer, ok := instance.GetConfig().GetTimer().(*roundtimer.RoundTimer)
	if ok {
		timer.OnTimeout(b.onTimeout(height))
	}
}

// onTimeout is trigger upon timeout for the given height
func (b *BaseRunner) onTimeout(h specqbft.Height) func() {
	return func() {
		if !b.hasRunningDuty() && b.QBFTController.Height == h {
			return
		}
		instance := b.State.RunningInstance
		if instance == nil {
			return
		}
		decided, _ := instance.IsDecided()
		if decided {
			return
		}
		err := instance.UponRoundTimeout()
		if err != nil {
			// TODO: handle?
			fmt.Println("failed to handle timeout:", err.Error())
		}
	}
}