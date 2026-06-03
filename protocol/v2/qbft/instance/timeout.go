package instance

import (
	"context"
	"encoding/hex"

	"github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/traces"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
)

func (i *Instance) UponRoundTimeout(ctx context.Context, logger *zap.Logger) error {
	ctx, span := tracer.Start(ctx, observability.InstrumentName(observabilityNamespace, "qbft.instance.round_timeout"))
	defer span.End()

	if !i.IsRelevant() {
		return types.WrapError(types.TimeoutInstanceErrorCode, traces.Errorf(span, "instance is no longer considered relevant"))
	}

	prevRound := i.State.Round
	newRound := prevRound + 1

	// Always move on to the next round. The round-change message broadcast is a best-effort thing, the QBFT
	// cluster as a whole can progress further even if our round-change message cannot be created/broadcast
	// for whatever reason — hence the bump is deferred, so it still runs even when CreateRoundChange/Broadcast
	// below return early with an error.
	//
	// Deferring also keeps State.Round at prevRound while we create and broadcast the round change. Bumping
	// eagerly would advance the instance into the cut-off round at the boundary (prevRound == CutOffRound-1),
	// at which point Broadcast rejects the message as no-longer-relevant and this final round-change — the one
	// the spec expects us to send — would never go out.
	defer i.bumpToRound(newRound)

	i.metrics.EndStage(ctx, prevRound)
	i.metrics.StartStage(stageRoundChange)
	i.metrics.RecordRoundChange(ctx, prevRound, reasonTimeout)

	startValueRoot := qbft.HashDataRoot(i.StartValue)
	logger = logger.With(zap.String("qbft_start_value_root", hex.EncodeToString(startValueRoot[:])))

	logger.Debug("⌛ round timed out")

	roundChange, err := i.CreateRoundChange(newRound)
	if err != nil {
		return traces.Errorf(span, "could not generate round change msg: %w", err)
	}

	const eventMsg = "📢 broadcasting round change message (this round timed out)"
	span.AddEvent(eventMsg, trace.WithAttributes(observability.BeaconBlockRootAttribute(startValueRoot), observability.DutyRoundAttribute(prevRound)))
	logger.Debug(
		eventMsg,
		zap.Uint64("qbft_new_round", uint64(newRound)),
		zap.Any("round_change_signers", roundChange.OperatorIDs),
	)

	if err := i.Broadcast(roundChange); err != nil {
		return traces.Errorf(span, "failed to broadcast round change message: %w", err)
	}

	return nil
}
