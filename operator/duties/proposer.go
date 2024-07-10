package duties

import (
	"context"
	"fmt"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
)

type ProposerHandler struct {
	baseHandler

	duties     *dutystore.Duties[eth2apiv1.ProposerDuty]
	fetchFirst bool
}

func NewProposerHandler(duties *dutystore.Duties[eth2apiv1.ProposerDuty]) *ProposerHandler {
	return &ProposerHandler{
		duties:     duties,
		fetchFirst: true,
	}
}

func (h *ProposerHandler) Name() string {
	return spectypes.BNRoleProposer.String()
}

// HandleDuties manages the duty lifecycle, handling different cases:
//
// On First Run:
//  1. Fetch duties for the current epoch.
//  2. Execute duties.
//
// On Re-org (current dependent root changed):
//  1. Fetch duties for the current epoch.
//  2. Execute duties.
//
// On Indices Change:
//  1. Execute duties.
//  2. ResetEpoch duties for the current epoch.
//  3. Fetch duties for the current epoch.
//
// On Ticker event:
//  1. Execute duties.
//  2. If necessary, fetch duties for the current epoch.
func (h *ProposerHandler) HandleDuties(ctx context.Context) {
	h.logger.Info("starting duty handler")
	defer h.logger.Info("duty handler exited")

	for {
		select {
		case <-ctx.Done():
			return

		case <-h.ticker.Next():
			slot := h.ticker.Slot()
			currentEpoch := h.network.Beacon.EstimatedEpochAtSlot(slot)
			buildStr := fmt.Sprintf("e%v-s%v-#%v", currentEpoch, slot, slot%32+1)
			h.logger.Debug("🛠 ticker event", zap.String("epoch_slot_pos", buildStr))

			ctx, cancel := context.WithDeadline(ctx, h.network.Beacon.GetSlotStartTime(slot+1).Add(100*time.Millisecond))
			if h.fetchFirst {
				h.fetchFirst = false
				h.indicesChanged = false
				h.processFetching(ctx, currentEpoch)
				h.processExecution(currentEpoch, slot)
			} else {
				h.processExecution(currentEpoch, slot)
				if h.indicesChanged {
					h.indicesChanged = false
					h.processFetching(ctx, currentEpoch)
				}
			}
			cancel()

			// last slot of epoch
			if uint64(slot)%h.network.Beacon.SlotsPerEpoch() == h.network.Beacon.SlotsPerEpoch()-1 {
				h.duties.ResetEpoch(currentEpoch - 1)
				h.fetchFirst = true
			}

		case reorgEvent := <-h.reorg:
			currentEpoch := h.network.Beacon.EstimatedEpochAtSlot(reorgEvent.Slot)
			buildStr := fmt.Sprintf("e%v-s%v-#%v", currentEpoch, reorgEvent.Slot, reorgEvent.Slot%32+1)
			h.logger.Info("🔀 reorg event received", zap.String("epoch_slot_pos", buildStr), zap.Any("event", reorgEvent))

			// reset current epoch duties
			if reorgEvent.Current {
				h.duties.ResetEpoch(currentEpoch)
				h.fetchFirst = true
			}

		case <-h.indicesChange:
			slot := h.network.Beacon.EstimatedCurrentSlot()
			currentEpoch := h.network.Beacon.EstimatedEpochAtSlot(slot)
			buildStr := fmt.Sprintf("e%v-s%v-#%v", currentEpoch, slot, slot%32+1)
			h.logger.Info("🔁 indices change received", zap.String("epoch_slot_pos", buildStr))

			h.indicesChanged = true
		}
	}
}

func (h *ProposerHandler) HandleInitialDuties(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, h.network.Beacon.SlotDurationSec()/2)
	defer cancel()

	epoch := h.network.Beacon.EstimatedCurrentEpoch()
	h.processFetching(ctx, epoch)
}

func (h *ProposerHandler) processFetching(ctx context.Context, epoch phase0.Epoch) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := h.fetchAndProcessDuties(ctx, epoch); err != nil {
		h.logger.Error("failed to fetch duties for current epoch", zap.Error(err))
		return
	}
}

func (h *ProposerHandler) processExecution(epoch phase0.Epoch, slot phase0.Slot) {
	duties := h.duties.CommitteeSlotDuties(epoch, slot)
	if duties == nil {
		return
	}

	// range over duties and execute
	toExecute := make([]*spectypes.BeaconDuty, 0, len(duties))
	for _, d := range duties {
		if h.shouldExecute(d) {
			toExecute = append(toExecute, h.toSpecDuty(d, spectypes.BNRoleProposer))
		}
	}
	h.dutiesExecutor.ExecuteDuties(h.logger, toExecute)
}

func (h *ProposerHandler) fetchAndProcessDuties(ctx context.Context, epoch phase0.Epoch) error {
	start := time.Now()

	allIndices := indicesFromShares(h.validatorProvider.ParticipatingValidators(epoch))
	if len(allIndices) == 0 {
		return nil
	}

	selfIndices := indicesFromShares(h.validatorProvider.SelfParticipatingValidators(epoch))
	selfIndicesSet := map[phase0.ValidatorIndex]struct{}{}
	for _, idx := range selfIndices {
		selfIndicesSet[idx] = struct{}{}
	}

	duties, err := h.beaconNode.ProposerDuties(ctx, epoch, allIndices)
	if err != nil {
		return fmt.Errorf("failed to fetch proposer duties: %w", err)
	}

	h.duties.ResetEpoch(epoch)

	specDuties := make([]*spectypes.BeaconDuty, 0, len(duties))
	for _, d := range duties {
		_, inCommitteeDuty := selfIndicesSet[d.ValidatorIndex]
		h.duties.Add(epoch, d.Slot, d.ValidatorIndex, d, inCommitteeDuty)
		specDuties = append(specDuties, h.toSpecDuty(d, spectypes.BNRoleProposer))
	}

	h.logger.Debug("📚 got duties",
		fields.Count(len(duties)),
		fields.Epoch(epoch),
		fields.Duties(epoch, specDuties),
		fields.Duration(start))

	return nil
}

func (h *ProposerHandler) toSpecDuty(duty *eth2apiv1.ProposerDuty, role spectypes.BeaconRole) *spectypes.BeaconDuty {
	return &spectypes.BeaconDuty{
		Type:           role,
		PubKey:         duty.PubKey,
		Slot:           duty.Slot,
		ValidatorIndex: duty.ValidatorIndex,
	}
}

func (h *ProposerHandler) shouldExecute(duty *eth2apiv1.ProposerDuty) bool {
	currentSlot := h.network.Beacon.EstimatedCurrentSlot()
	// execute task if slot already began and not pass 1 slot
	if currentSlot == duty.Slot {
		return true
	}
	if currentSlot+1 == duty.Slot {
		h.warnMisalignedSlotAndDuty(duty.String())
		return true
	}
	return false
}
