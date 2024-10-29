package instance

import (
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv/logging/fields"
	"go.uber.org/zap"
)

func (i *Instance) UponRoundTimeout(logger *zap.Logger) error {
	if !i.CanProcessMessages() {
		return errors.New("instance stopped processing timeouts")
	}

	newRound := i.State.Round + 1

	logger = logger.With(
		fields.Height(i.State.Height),
		fields.Round(i.State.Round),
		zap.Uint64("new_round", uint64(newRound)),
	)

	logger.Debug("⌛ round timed out, readying next round")

	roundChange, err := i.CreateRoundChange(i.signer, newRound)
	if err != nil {
		return errors.Wrap(err, "could not generate round change msg")
	}

	root, err := specqbft.HashDataRoot(i.StartValue)
	if err != nil {
		return err
	}
	logger.Debug("📢 broadcasting round change message",
		fields.Root(root),
		zap.Any("round_change_signers", roundChange.OperatorIDs),
		zap.String("reason", "timeout"))

	if err := i.Broadcast(logger, roundChange); err != nil {
		return errors.Wrap(err, "failed to broadcast round change message")
	}

	i.bumpToRound(newRound)
	i.State.ProposalAcceptedForCurrentRound = nil
	i.config.GetTimer().TimeoutForRound(i.State.Height, newRound)

	return nil
}
