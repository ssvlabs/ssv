package instance

import (
	"context"
	"encoding/hex"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/traces"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	"github.com/ssvlabs/ssv/qa/faults"
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

	// If that bump reached the cutoff round, give up rather than broadcast a round-change no node accepts.
	if !i.IsRelevant() {
		i.qaProposerRound3(logger, newRound)
		i.recordCutoffGiveUp(ctx, logger)
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

// qaProposerRound3 is QA fault menu TIM-08 (proposer-round-3), the ignore leg of the proposer round
// cap (ssv#3041: roundtimer.MaxRound(RoleProposer) = 2, so the proposer's cutoff is 3). Called only
// on the cutoff branch of UponRoundTimeout, after the bump; a no-op unless FAULT=proposer-round-3.
//
// On a PROPOSER instance whose round-2 timer just expired (the bump landed on round 3), it builds the
// round change the stock code deliberately does not send — RoundChange(3), via CreateRoundChange, so
// it carries this operator's valid signature and the current prepared justifications — and hands it
// straight to the network. Instance.Broadcast cannot be used: it refuses an instance that is no longer
// relevant, which this one now is. The caller then records the give-up as usual, so this node's own
// instance still stops: no timer, no further state change.
//
// Fires at most once per instance: reaching this branch needs Round < cutoff before the bump, and the
// round never moves back, so a later timeout returns at UponRoundTimeout's not-relevant guard.
//
// Receivers IGNORE it in message validation (`round is too high for this role, got 3 (PROPOSER
// role), want 2 (PROPOSER role)`), and so does this node's own pubsub validator — a routine
// self-ignore at Debug — so it only leaves the node with FAULT_NO_LOCAL_VALIDATION=true. The
// broadcast is asynchronous: a nil error (and faults.Fired) does not prove the message left.
func (i *Instance) qaProposerRound3(logger *zap.Logger, newRound specqbft.Round) {
	if !faults.Is(faults.ProposerRound3) || newRound != 3 || len(i.State.ID) != 56 {
		return
	}
	role := types.MessageID(i.State.ID).GetRoleType()
	if role != types.RoleProposer {
		return
	}

	roundChange, err := i.CreateRoundChange(newRound)
	if err != nil {
		logger.Warn("🧪 qa fault could not build RoundChange(3)",
			zap.String("qa_fault", string(faults.Active())), zap.Error(err))
		return
	}

	// The same line the stock path logs, so the TIM-08 card's grep on the faulted node holds.
	logger.Debug(
		"📢 broadcasting round change message (this round timed out)",
		zap.Uint64("qbft_new_round", uint64(newRound)),
		zap.Any("round_change_signers", roundChange.OperatorIDs),
	)

	slot := phase0.Slot(i.State.Height)
	if err := i.GetConfig().GetNetwork().BroadcastAtSlot(roundChange, slot); err != nil {
		logger.Warn("🧪 qa fault could not broadcast RoundChange(3)",
			zap.String("qa_fault", string(faults.Active())), zap.Error(err))
		return
	}
	faults.Fired(logger,
		zap.Uint64("slot", uint64(slot)),
		zap.Uint64("qbft_round", uint64(newRound)),
		zap.String("role", message.RunnerRoleToString(role)),
	)
}
