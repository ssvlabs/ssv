package duties

import (
	"context"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log/fields"
)

// PTCAttestationHandler schedules the Gloas (ePBS) Payload Timeliness Committee attestation duty
// (SIP #94 §3): it fetches PTC duties per epoch and, for each slot holding one, executes it at the
// 75%-of-slot cutoff so the runner observes payload presence at that point and runs its
// partial-signature round in the otherwise-free [75%, 100%] window.
type PTCAttestationHandler struct {
	baseHandler

	// duties caches fetched duties as ready-to-execute ValidatorDuties, keyed by epoch then slot.
	// Accessed only from the HandleDuties goroutine.
	duties map[phase0.Epoch]map[phase0.Slot][]*spectypes.ValidatorDuty
}

func NewPTCAttestationHandler() *PTCAttestationHandler {
	return &PTCAttestationHandler{
		duties: map[phase0.Epoch]map[phase0.Slot][]*spectypes.ValidatorDuty{},
	}
}

func (h *PTCAttestationHandler) Name() string {
	return spectypes.BNRolePTCAttester.String()
}

func (h *PTCAttestationHandler) WaitShutdown() {}

func (h *PTCAttestationHandler) HandleDuties(ctx context.Context) {
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

			// PTC is a Gloas-only duty.
			if !h.netCfg.IsGloas(epoch) {
				continue
			}

			h.fetchDuties(ctx, epoch)
			if h.shouldFetchNextEpoch(slot) {
				h.fetchDuties(ctx, epoch+1)
			}
			h.evictOutdated(epoch)

			if duties := h.duties[epoch][slot]; len(duties) > 0 {
				h.scheduleExecution(ctx, slot, duties)
			}

		case <-h.indicesChangeCh:
		case <-h.reorgEventsCh:
		}
	}
}

// fetchDuties fetches and caches an epoch's PTC duties once.
func (h *PTCAttestationHandler) fetchDuties(ctx context.Context, epoch phase0.Epoch) {
	if _, cached := h.duties[epoch]; cached {
		return
	}

	shares := h.validatorProvider.SelfParticipatingValidators(epoch)
	indices := make([]phase0.ValidatorIndex, 0, len(shares))
	for _, share := range shares {
		indices = append(indices, share.ValidatorIndex)
	}
	if len(indices) == 0 {
		return
	}

	ptcDuties, err := h.beaconNode.PayloadAttestationDuties(ctx, epoch, indices)
	if err != nil {
		h.logger.Warn("failed to fetch PTC duties", fields.Epoch(epoch), zap.Error(err))
		return
	}

	bySlot := make(map[phase0.Slot][]*spectypes.ValidatorDuty)
	for _, d := range ptcDuties {
		bySlot[d.Slot] = append(bySlot[d.Slot], &spectypes.ValidatorDuty{
			Type:           spectypes.BNRolePTCAttester,
			PubKey:         d.PubKey,
			ValidatorIndex: d.ValidatorIndex,
			Slot:           d.Slot,
		})
	}
	h.duties[epoch] = bySlot

	h.logger.Debug("fetched PTC duties", fields.Epoch(epoch), zap.Int("duties", len(ptcDuties)))
}

// scheduleExecution fires the duty at the 75%-of-slot cutoff (PAYLOAD_ATTESTATION_DUE_BPS), with a
// deadline at slot end.
func (h *PTCAttestationHandler) scheduleExecution(ctx context.Context, slot phase0.Slot, duties []*spectypes.ValidatorDuty) {
	executeAt := h.netCfg.SlotStartTime(slot).Add(h.netCfg.SlotDuration * 3 / 4)
	deadline := h.netCfg.SlotStartTime(slot + 1)
	time.AfterFunc(time.Until(executeAt), func() {
		h.dutiesExecutor.ExecuteDuties(ctx, duties, deadline)
	})
}

// evictOutdated drops cached duties for epochs before the current one.
func (h *PTCAttestationHandler) evictOutdated(currentEpoch phase0.Epoch) {
	for epoch := range h.duties {
		if epoch < currentEpoch {
			delete(h.duties, epoch)
		}
	}
}
