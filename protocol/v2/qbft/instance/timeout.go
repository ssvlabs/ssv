package instance

import (
	"context"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/observability"
)

func (i *Instance) UponRoundTimeout(ctx context.Context, logger *zap.Logger) error {
	ctx, span := tracer.Start(ctx, observability.InstrumentName(observabilityNamespace, "qbft.instance.round_timeout"))
	defer span.End()

	if !i.CanProcessMessages() {
		return observability.Errorf(span, "instance stopped processing timeouts")
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

	roundChange, err := CreateRoundChange(i.State, i.signer, newRound)
	if err != nil {
		return observability.Errorf(span, "could not generate round change msg: %w", err)
	}

	root, err := specqbft.HashDataRoot(i.StartValue)
	if err != nil {
		return observability.Errorf(span, "could not calculate root for round change: %w", err)
	}

	i.metrics.RecordRoundChange(ctx, newRound, reasonTimeout)

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

	if err := i.Broadcast(roundChange); err != nil {
		return observability.Errorf(span, "failed to broadcast round change message: %w", err)
	}

	return nil
}
