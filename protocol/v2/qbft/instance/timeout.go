package instance

import (
	"context"

	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/observability"
)

func (i *Instance) UponRoundTimeout(ctx context.Context, logger *zap.Logger) error {
	ctx, span := tracer.Start(ctx, observability.InstrumentName(observabilityNamespace, "round_timeout"))
	defer span.End()

	if !i.CanProcessMessages() {
		err := errors.New("instance stopped processing timeouts")
		span.SetStatus(codes.Error, err.Error())
		return err
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
		err := errors.Wrap(err, "could not generate round change msg")
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	root, err := specqbft.HashDataRoot(i.StartValue)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	const eventMsg = "📢 broadcasting round change message"
	span.AddEvent(eventMsg,
		trace.WithAttributes(
			observability.BeaconBlockRootAttribute(root),
			observability.DutyRoundAttribute(i.State.Round),
		))

	logger.Debug(eventMsg,
		fields.Round(i.State.Round),
		fields.Root(root),
		zap.Any("round_change_signers", roundChange.OperatorIDs),
		fields.Height(i.State.Height),
		zap.String("reason", "timeout"))

	if err := i.Broadcast(logger, roundChange); err != nil {
		err := errors.Wrap(err, "failed to broadcast round change message")
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	return nil
}
