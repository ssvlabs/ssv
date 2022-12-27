package runner

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"go.uber.org/zap"

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

		runningInstance := b.State.GetRunningInstance()
		if runningInstance == nil {
			return
		}

		decided, _ := runningInstance.IsDecided()
		if decided {
			return
		}

		err := runningInstance.UponRoundTimeout()
		if err != nil {
			// TODO: handle?
			b.logger.Warn("failed to handle timeout", zap.Error(err))
		}
	}
}
