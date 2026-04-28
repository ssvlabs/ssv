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

	i.metrics.EndStage(ctx, prevRound)
	i.metrics.StartStage(stageRoundChange)
	i.metrics.RecordRoundChange(ctx, prevRound, reasonTimeout)

	startValueRoot := qbft.HashDataRoot(i.StartValue)
	logger = logger.With(zap.String("qbft_start_value_root", hex.EncodeToString(startValueRoot[:])))

	logger.Debug("⌛ round timed out")

	// Always move on to the next round. The round-change message broadcast is a best-effort thing, the QBFT
	// cluster as a whole can progress further even if our round-change message cannot be created/broadcast
	// for whatever reason.
	i.bumpToRound(newRound)
	i.State.ProposalAcceptedForCurrentRound = nil

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
