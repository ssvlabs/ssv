package instance

import (
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func (i *Instance) UponRoundTimeout(logger *zap.Logger) error {
	newRound := i.State.Round + 1
	logger.Debug("⌛ round timed out", fields.Round(newRound))
	i.bumpToRound(newRound)
	i.State.ProposalAcceptedForCurrentRound = nil
	i.config.GetTimer().TimeoutForRound(i.State.Round)

	roundChange, err := CreateRoundChange(i.State, i.config, newRound, i.StartValue)
	if err != nil {
		return errors.Wrap(err, "could not generate round change msg")
	}

	logger.Debug("📢 broadcasting round change message",
		fields.Round(i.State.Round),
		fields.Root(roundChange.Message.Root),
		zap.Any("round-change-signers", roundChange.Signers),
		fields.Height(i.State.Height),
		zap.String("reason", "timeout"))

	if err := i.Broadcast(logger, roundChange); err != nil {
		return errors.Wrap(err, "failed to broadcast round change message")
	}

	return nil
}
