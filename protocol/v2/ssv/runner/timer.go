package runner

import (
	"context"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
)

type TimeoutF func(ctx context.Context, logger *zap.Logger, identifier spectypes.MessageID, height specqbft.Height) roundtimer.OnRoundTimeoutF

func (b *BaseRunner) createTimer(ctx context.Context, logger *zap.Logger, height specqbft.Height) specqbft.Timer {
	if b.timerCancel != nil {
		b.timerCancel()
	}
	ctx, cancel := context.WithCancel(ctx)
	b.timerCancel = cancel
	identifier := spectypes.MessageID(b.QBFTController.GetIdentifier())
	callback := b.TimeoutF(ctx, logger, identifier, height)
	return roundtimer.New(ctx, b.NetworkConfig.Beacon, b.RunnerRoleType, height, callback)
}
