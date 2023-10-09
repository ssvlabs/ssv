package instance

import (
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
)

var CutoffRound = 15 // stop processing instances after 8*2+120*6 = 14.2 min (~ 2 epochs)

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
