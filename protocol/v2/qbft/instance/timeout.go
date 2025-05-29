package instance

import (
	"context"

	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
)

func (i *Instance) UponRoundTimeout(ctx context.Context, logger *zap.Logger) error {
	if !i.CanProcessMessages() {
		return errors.New("instance stopped processing timeouts")
	}

	newRound := i.State.Round + 1
	logger.Debug("⌛ round timed out", fields.Round(newRound))

	// TODO: previously this was done outside of a defer, which caused the
	// round to be bumped before the round change message was created & broadcasted.
	// Remember to track the impact of this change and revert/modify if necessary.
	defer func() {
		i.bumpToRound(ctx, newRound)
		i.State.ProposalAcceptedForCurrentRound = nil
		i.config.GetTimer().TimeoutForRound(i.State.Height, i.State.Round)
	}()

	roundChange, err := CreateRoundChange(i.State, i.signer, newRound, i.StartValue)
	if err != nil {
		return errors.Wrap(err, "could not generate round change msg")
	}

	root, err := specqbft.HashDataRoot(i.StartValue)
	if err != nil {
		return err
	}

	i.metrics.RecordRoundChange(ctx, newRound, reasonTimeout)

	logger.Debug("📢 broadcasting round change message",
		fields.Round(i.State.Round),
		fields.Root(root),
		zap.Any("round_change_signers", roundChange.OperatorIDs),
		fields.Height(i.State.Height),
		zap.String("reason", "timeout"))

	if err := i.Broadcast(logger, roundChange); err != nil {
		return errors.Wrap(err, "failed to broadcast round change message")
	}

	return nil
}
