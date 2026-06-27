package duties

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log/fields"
)

// ProposerPreferencesHandler schedules the Gloas (ePBS) proposer-preferences duty (SIP #94 §5): for
// each upcoming proposal slot a local validator holds within the proposer lookahead, it emits one
// duty so the runner broadcasts that validator's fee recipient and target gas limit ahead of the
// slot. Unlike slot-bound duties it emits in advance — duty.Slot is the future proposal slot, and the
// duty executes (the runner signs and broadcasts) as soon as the assignment is known.
type ProposerPreferencesHandler struct {
	baseHandler

	// processed records epochs already fetched and handled (preferences emitted, or confirmed to hold
	// no local proposals), so each epoch fires once. Accessed only from the HandleDuties goroutine.
	processed map[phase0.Epoch]struct{}
}

func NewProposerPreferencesHandler() *ProposerPreferencesHandler {
	return &ProposerPreferencesHandler{
		processed: map[phase0.Epoch]struct{}{},
	}
}

func (h *ProposerPreferencesHandler) Name() string {
	return spectypes.BNRoleProposerPreferences.String()
}

func (h *ProposerPreferencesHandler) WaitShutdown() {}

// HandleDuties emits proposer-preferences duties across the proposer lookahead (current + next epoch,
// MIN_SEED_LOOKAHEAD=1). In the epoch immediately before the Gloas fork it pre-emits the first Gloas
// epoch's preferences (SIP #94 §5) so builders have them before the fork. Reorg/indices-change re-emission
// is handled below; the publication-finality hold is a deferred refinement.
func (h *ProposerPreferencesHandler) HandleDuties(ctx context.Context) {
	h.logger.Info("starting duty handler")
	defer h.logger.Info("duty handler exited")

	next := h.ticker.Next()
	for {
		select {
		case <-ctx.Done():
			return

		case <-next:
			slot := h.ticker.Slot()
			next = h.ticker.Next()
			h.emitForTick(ctx, slot)

		case <-h.indicesChangeCh:
			h.reEmitLookahead("indices change")

		case <-h.reorgEventsCh:
			h.reEmitLookahead("reorg")
		}
	}
}

// reEmitLookahead drops the emitted-epoch markers so the next tick re-fetches and re-emits the
// lookahead's preferences — after a reorg (new dependent_root) or a validator-set change (new local
// validators that missed an already-processed epoch). Per SIP #94 §5 a changed dependent_root yields a
// distinct gossip tuple, not a replacement; re-emitting an unchanged tuple is harmless (gossip keeps
// only the first).
func (h *ProposerPreferencesHandler) reEmitLookahead(reason string) {
	h.logger.Debug("🔀 re-emitting proposer preferences on next tick", zap.String("reason", reason))
	clear(h.processed)
}

// emitForTick emits the lookahead's preferences for the tick's slot: the current epoch (plus the next,
// once it's a good time to fetch) in steady state, or the first Gloas epoch when in the pre-fork
// window. Outside both it does nothing (pre-Gloas, no preferences yet).
func (h *ProposerPreferencesHandler) emitForTick(ctx context.Context, slot phase0.Slot) {
	epoch := h.netCfg.EstimatedEpochAtSlot(slot)
	switch {
	case h.netCfg.IsGloas(epoch):
		h.emitForEpoch(ctx, epoch, slot)
		if h.shouldFetchNextEpoch(slot) {
			h.emitForEpoch(ctx, epoch+1, slot)
		}
		h.evictOutdated(epoch)
	case h.netCfg.InGloasPriorWindow(slot):
		// epoch+1 is GLOAS_FORK_EPOCH throughout the prior window (MIN_SEED_LOOKAHEAD=1).
		h.emitForEpoch(ctx, epoch+1, slot)
	}
}

// emitForEpoch fetches the epoch's proposer assignments for local validators once and emits one
// proposer-preferences duty per assignment, to be executed (broadcast) immediately.
func (h *ProposerPreferencesHandler) emitForEpoch(ctx context.Context, epoch phase0.Epoch, currentSlot phase0.Slot) {
	if _, done := h.processed[epoch]; done {
		return
	}

	indices := h.selfParticipatingIndices(epoch)
	if len(indices) == 0 {
		return // no local validators yet; retry on the next tick
	}

	duties, err := h.beaconNode.ProposerDuties(ctx, epoch, indices)
	if err != nil {
		h.logger.Warn("failed to fetch proposer duties", fields.Epoch(epoch), zap.Error(err))
		return // retry on the next tick
	}

	preferenceDuties := make([]*spectypes.ValidatorDuty, 0, len(duties))
	for _, d := range duties {
		preferenceDuties = append(preferenceDuties, &spectypes.ValidatorDuty{
			Type:           spectypes.BNRoleProposerPreferences,
			PubKey:         d.PubKey,
			Slot:           d.Slot, // proposal slot — the self-identifying duty.Slot
			ValidatorIndex: d.ValidatorIndex,
		})
	}
	h.processed[epoch] = struct{}{}

	if len(preferenceDuties) == 0 {
		return
	}

	// Emit now: the runner builds, signs, and broadcasts immediately. duty.Slot is the (future)
	// proposal slot, so bound execution by the current slot, not duty.Slot.
	deadline := h.netCfg.SlotStartTime(currentSlot + 1)
	h.dutiesExecutor.ExecuteDuties(ctx, preferenceDuties, deadline)

	h.logger.Debug("emitted proposer preferences duties",
		fields.Epoch(epoch),
		fields.Count(len(preferenceDuties)),
	)
}

// evictOutdated drops processed-epoch markers for epochs before the current one.
func (h *ProposerPreferencesHandler) evictOutdated(currentEpoch phase0.Epoch) {
	evictEpochsBefore(h.processed, currentEpoch)
}
