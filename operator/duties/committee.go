package duties

import (
	"context"
	"fmt"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/operator/duties/dutystore"
)

type committeeDutiesMap map[spectypes.CommitteeID]*spectypes.CommitteeDuty

type CommitteeHandler struct {
	baseHandler

	attDuties  *dutystore.Duties[eth2apiv1.AttesterDuty]
	syncDuties *dutystore.SyncCommitteeDuties
}

func NewCommitteeHandler(attDuties *dutystore.Duties[eth2apiv1.AttesterDuty], syncDuties *dutystore.SyncCommitteeDuties) *CommitteeHandler {
	h := &CommitteeHandler{
		attDuties:  attDuties,
		syncDuties: syncDuties,
	}

	return h
}

func (h *CommitteeHandler) Name() string {
	// TODO(Oleg): change to spectypes.BNRoleCluster.String() after spec alignment ?
	return "CLUSTER"
}

// HandleDuties manages the duty lifecycle, handling different cases:
//
// On First Run:
//  1. Fetch duties for the current epoch.
//  2. If necessary, fetch duties for the next epoch.
//  3. Execute duties.
//
// On Re-org:
//
//	If the previous dependent root changed:
//	    1. Fetch duties for the current epoch.
//	    2. Execute duties.
//	If the current dependent root changed:
//	    1. Execute duties.
//	    2. If necessary, fetch duties for the next epoch.
//
// On Indices Change:
//  1. Execute duties.
//  2. ResetEpoch duties for the current epoch.
//  3. Fetch duties for the current epoch.
//  4. If necessary, fetch duties for the next epoch.
//
// On Ticker event:
//  1. Execute duties.
//  2. If necessary, fetch duties for the next epoch.
func (h *CommitteeHandler) HandleDuties(ctx context.Context) {
	h.logger.Info("starting duty handler")
	defer h.logger.Info("duty handler exited")

	for {
		select {
		case <-ctx.Done():
			return

		case <-h.ticker.Next():
			slot := h.ticker.Slot()
			epoch := h.network.Beacon.EstimatedEpochAtSlot(slot)
			period := h.network.Beacon.EstimatedSyncCommitteePeriodAtEpoch(epoch)
			buildStr := fmt.Sprintf("p%v-e%v-s%v-#%v", period, epoch, slot, slot%32+1)
			h.logger.Debug("🛠 ticker event", zap.String("period_epoch_slot_pos", buildStr))

			h.processExecution(period, epoch, slot)

			// cleanups
			slotsPerEpoch := h.network.Beacon.SlotsPerEpoch()
			// last slot of epoch
			if uint64(slot)%slotsPerEpoch == slotsPerEpoch-1 {
				h.attDuties.ResetEpoch(epoch)
			}

			// last slot of period
			if slot == h.network.Beacon.LastSlotOfSyncPeriod(period) {
				h.syncDuties.Reset(period - 1)
			}

		case reorgEvent := <-h.reorg:
			currentEpoch := h.network.Beacon.EstimatedEpochAtSlot(reorgEvent.Slot)
			buildStr := fmt.Sprintf("e%v-s%v-#%v", currentEpoch, reorgEvent.Slot, reorgEvent.Slot%32+1)
			h.logger.Info("🔀 reorg event received", zap.String("epoch_slot_pos", buildStr), zap.Any("event", reorgEvent))

			//// reset current epoch duties
			//if reorgEvent.Previous {
			//	h.duties.ResetEpoch(currentEpoch)
			//	h.fetchFirst = true
			//	h.fetchCurrentEpoch = true
			//	if h.shouldFetchNexEpoch(reorgEvent.Slot) {
			//		h.duties.ResetEpoch(currentEpoch + 1)
			//		h.fetchNextEpoch = true
			//	}
			//} else if reorgEvent.Current {
			//	// reset & re-fetch next epoch duties if in appropriate slot range,
			//	// otherwise they will be fetched by the appropriate slot tick.
			//	if h.shouldFetchNexEpoch(reorgEvent.Slot) {
			//		h.duties.ResetEpoch(currentEpoch + 1)
			//		h.fetchNextEpoch = true
			//	}
			//}

		case <-h.indicesChange:
			slot := h.network.Beacon.EstimatedCurrentSlot()
			currentEpoch := h.network.Beacon.EstimatedEpochAtSlot(slot)
			buildStr := fmt.Sprintf("e%v-s%v-#%v", currentEpoch, slot, slot%32+1)
			h.logger.Info("🔁 indices change received", zap.String("epoch_slot_pos", buildStr))

			//h.indicesChanged = true
			//h.fetchCurrentEpoch = true
			//
			//// reset next epoch duties if in appropriate slot range
			//if h.shouldFetchNexEpoch(slot) {
			//	h.duties.ResetEpoch(currentEpoch + 1)
			//	h.fetchNextEpoch = true
			//}
		}
	}
}

func (h *CommitteeHandler) processExecution(period uint64, epoch phase0.Epoch, slot phase0.Slot) {
	attDuties := h.attDuties.CommitteeSlotDuties(epoch, slot)
	syncDuties := h.syncDuties.CommitteePeriodDuties(period)
	if attDuties == nil && syncDuties == nil {
		return
	}

	committeeMap := h.buildCommitteeDuties(attDuties, syncDuties, slot)
	h.executeCommitteeDuties(h.logger, committeeMap)
}

func (h *CommitteeHandler) buildCommitteeDuties(attDuties []*eth2apiv1.AttesterDuty, syncDuties []*eth2apiv1.SyncCommitteeDuty, slot phase0.Slot) committeeDutiesMap {
	committeeMap := make(committeeDutiesMap)

	for _, d := range attDuties {
		if h.shouldExecuteAtt(d) {
			specDuty := h.toSpecAttDuty(d, spectypes.BNRoleAttester)
			h.appendBeaconDuty(committeeMap, specDuty)
		}
	}

	for _, d := range syncDuties {
		if h.shouldExecuteSync(d, slot) {
			specDuty := h.toSpecSyncDuty(d, slot, spectypes.BNRoleSyncCommittee)
			h.appendBeaconDuty(committeeMap, specDuty)
		}
	}

	return committeeMap
}

func (h *CommitteeHandler) appendBeaconDuty(m committeeDutiesMap, beaconDuty *spectypes.BeaconDuty) {
	clusterID := h.validatorProvider.Validator(beaconDuty.PubKey[:]).CommitteeID()
	if _, ok := m[clusterID]; !ok {
		m[clusterID] = &spectypes.CommitteeDuty{
			Slot:         beaconDuty.Slot,
			BeaconDuties: make([]*spectypes.BeaconDuty, 0),
		}
	}
	m[clusterID].BeaconDuties = append(m[clusterID].BeaconDuties, beaconDuty)
}

func (h *CommitteeHandler) toSpecAttDuty(duty *eth2apiv1.AttesterDuty, role spectypes.BeaconRole) *spectypes.BeaconDuty {
	return &spectypes.BeaconDuty{
		Type:                    role,
		PubKey:                  duty.PubKey,
		Slot:                    duty.Slot,
		ValidatorIndex:          duty.ValidatorIndex,
		CommitteeIndex:          duty.CommitteeIndex,
		CommitteeLength:         duty.CommitteeLength,
		CommitteesAtSlot:        duty.CommitteesAtSlot,
		ValidatorCommitteeIndex: duty.ValidatorCommitteeIndex,
	}
}

func (h *CommitteeHandler) toSpecSyncDuty(duty *eth2apiv1.SyncCommitteeDuty, slot phase0.Slot, role spectypes.BeaconRole) *spectypes.BeaconDuty {
	indices := make([]uint64, len(duty.ValidatorSyncCommitteeIndices))
	for i, index := range duty.ValidatorSyncCommitteeIndices {
		indices[i] = uint64(index)
	}
	return &spectypes.BeaconDuty{
		Type:                          role,
		PubKey:                        duty.PubKey,
		Slot:                          slot, // in order for the duty scheduler to execute
		ValidatorIndex:                duty.ValidatorIndex,
		ValidatorSyncCommitteeIndices: indices,
	}
}

func (h *CommitteeHandler) shouldExecuteAtt(duty *eth2apiv1.AttesterDuty) bool {
	currentSlot := h.network.Beacon.EstimatedCurrentSlot()
	// execute task if slot already began and not pass 1 epoch
	var attestationPropagationSlotRange = phase0.Slot(h.network.Beacon.SlotsPerEpoch())
	if currentSlot >= duty.Slot && currentSlot-duty.Slot <= attestationPropagationSlotRange {
		return true
	}
	if currentSlot+1 == duty.Slot {
		h.warnMisalignedSlotAndDuty(duty.String())
		return true
	}
	return false
}

func (h *CommitteeHandler) shouldExecuteSync(duty *eth2apiv1.SyncCommitteeDuty, slot phase0.Slot) bool {
	currentSlot := h.network.Beacon.EstimatedCurrentSlot()
	// execute task if slot already began and not pass 1 slot
	if currentSlot == slot {
		return true
	}
	if currentSlot+1 == slot {
		h.warnMisalignedSlotAndDuty(duty.String())
		return true
	}
	return false
}
