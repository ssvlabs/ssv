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

// HandleDuties emits proposer-preferences duties for the current and (once it's a good time to fetch)
// the next epoch — the MIN_SEED_LOOKAHEAD=1 proposer lookahead. Reorg-driven re-emission and the
// publication-finality hold (SIP #94 §5) are deferred refinements; the pre-fork emission window is
// handled with the fork cutover.
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
			epoch := h.netCfg.EstimatedEpochAtSlot(slot)

			// Proposer preferences are a Gloas-only duty.
			if !h.netCfg.IsGloas(epoch) {
				continue
			}

			h.emitForEpoch(ctx, epoch, slot)
			if h.shouldFetchNextEpoch(slot) {
				h.emitForEpoch(ctx, epoch+1, slot)
			}
			h.evictOutdated(epoch)

		case <-h.indicesChangeCh:
		case <-h.reorgEventsCh:
		}
	}
}

// emitForEpoch fetches the epoch's proposer assignments for local validators once and emits one
// proposer-preferences duty per assignment, to be executed (broadcast) immediately.
func (h *ProposerPreferencesHandler) emitForEpoch(ctx context.Context, epoch phase0.Epoch, currentSlot phase0.Slot) {
	if _, done := h.processed[epoch]; done {
		return
	}

	shares := h.validatorProvider.SelfParticipatingValidators(epoch)
	indices := make([]phase0.ValidatorIndex, 0, len(shares))
	for _, share := range shares {
		indices = append(indices, share.ValidatorIndex)
	}
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
	for epoch := range h.processed {
		if epoch < currentEpoch {
			delete(h.processed, epoch)
		}
	}
}
