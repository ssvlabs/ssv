package instance

import (
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func (i *Instance) UponRoundTimeout(logger *zap.Logger) error {
	newRound := i.State.Round + 1
	logger.Debug("⌛ round timed out", zap.Uint64("round", uint64(newRound)))
	i.bumpToRound(newRound)
	i.State.ProposalAcceptedForCurrentRound = nil
	i.config.GetTimer().TimeoutForRound(i.State.Round)

	roundChange, err := CreateRoundChange(i.State, i.config, newRound, i.StartValue)
	if err != nil {
		return errors.Wrap(err, "could not generate round change msg")
	}

	if err := i.Broadcast(logger, roundChange); err != nil {
		return errors.Wrap(err, "failed to broadcast round change message")
	}

	return nil
}
