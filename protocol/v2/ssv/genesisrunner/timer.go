package genesisrunner

import (
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"github.com/ssvlabs/ssv/protocol/v2/genesisqbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/genesisqbft/roundtimer"
	"go.uber.org/zap"
)

type TimeoutF func(logger *zap.Logger, identifier genesisspectypes.MessageID, height genesisspecqbft.Height) roundtimer.OnRoundTimeoutF

func (b *BaseRunner) registerTimeoutHandler(logger *zap.Logger, instance *instance.Instance, height genesisspecqbft.Height) {
	identifier := genesisspectypes.MessageIDFromBytes(instance.State.ID)
	timer, ok := instance.GetConfig().GetTimer().(*roundtimer.RoundTimer)
	if ok {
		timer.OnTimeout(b.TimeoutF(logger, identifier, height))
	}
}
