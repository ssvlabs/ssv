package instance

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
)

func (i *Instance) UponRoundTimeout(logger *zap.Logger) error {
	if !i.CanProcessMessages() {
		return errors.New("instance stopped processing timeouts")
	}

	newRound := i.State.Round + 1
	logger.Debug("⌛ round timed out", fields.Round(newRound))

	// TODO: previously this was done outside of a defer, which caused the
	// round to be bumped before the round change message was created & broadcasted.
	// Remember to track the impact of this change and revert/modify if necessary.
	defer func() {
		i.bumpToRound(newRound)
		i.State.ProposalAcceptedForCurrentRound = nil
		i.config.GetTimer().TimeoutForRound(i.State.Height, i.State.Round)
	}()

	roundChange, err := CreateRoundChange(i.State, i.config, newRound, i.StartValue)
	if err != nil {
		return errors.Wrap(err, "could not generate round change msg")
	}

	roundChangeMsg, err := specqbft.DecodeMessage(roundChange.SSVMessage.Data)
	if err != nil {
		return err
	}

	logger.Debug("📢 broadcasting round change message",
		fields.Round(i.State.Round),
		fields.Root(roundChangeMsg.Root),
		zap.Any("round-change-signers", roundChange.GetOperatorIDs()),
		fields.Height(i.State.Height),
		zap.String("reason", "timeout"))

	if err := i.Broadcast(roundChange); err != nil {
		return errors.Wrap(err, "failed to broadcast round change message")
	}

	return nil
}
