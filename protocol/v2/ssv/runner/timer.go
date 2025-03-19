package runner

import (
	"context"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
)

type TimeoutF func(ctx context.Context, logger *zap.Logger, identifier spectypes.MessageID, height specqbft.Height) roundtimer.OnRoundTimeoutF

func (b *BaseRunner) registerTimeoutHandler(ctx context.Context, logger *zap.Logger, instance *instance.Instance, height specqbft.Height) {
	identifier := spectypes.MessageID(instance.State.ID)
	timer, ok := instance.GetConfig().GetTimer().(*roundtimer.RoundTimer)
	if ok {
		timer.OnTimeout(b.TimeoutF(ctx, logger, identifier, height))
	}
}
