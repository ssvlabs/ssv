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

	// Move on to the next round; the round-change broadcast below is best-effort (the cluster can progress
	// without ours). We bump *before* the broadcast, unlike ssv-spec which defers it.
	i.bumpToRound(newRound)

	// If the bump reached the role's give-up round (roundtimer.CutOffRoundFor), the instance stops here:
	// no timer was armed and we broadcast no round-change (worthless past the cutoff, where no node
	// decides). This is a normal end, not an error, so return nil rather than redden the surrounding spans.
	if !i.IsRelevant() {
		const eventMsg = "instance reached its cutoff round, giving up"
		span.AddEvent(eventMsg)
		logger.Debug(eventMsg, zap.Uint64("qbft_round", uint64(i.State.Round)))
		return nil
	}

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
