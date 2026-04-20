package runner

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
)

type TimeoutF func(ctx context.Context, logger *zap.Logger, identifier spectypes.MessageID, slot phase0.Slot) roundtimer.OnRoundTimeoutF

func (b *BaseRunner) createTimer(ctx context.Context, logger *zap.Logger, slot phase0.Slot) specqbft.Timer {
	if b.timerCancel != nil {
		b.timerCancel()
	}
	ctx, cancel := context.WithCancel(ctx)
	b.timerCancel = cancel
	identifier := spectypes.MessageID(b.QBFTController.GetIdentifier())
	callback := b.TimeoutF(ctx, logger, identifier, slot)
	return roundtimer.New(ctx, b.NetworkConfig.Beacon, b.RunnerRoleType, slot, callback)
}
