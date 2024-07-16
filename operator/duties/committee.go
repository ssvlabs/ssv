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

	validatorCommitteeIDs map[phase0.ValidatorIndex]spectypes.CommitteeID
}

func NewCommitteeHandler(attDuties *dutystore.Duties[eth2apiv1.AttesterDuty], syncDuties *dutystore.SyncCommitteeDuties) *CommitteeHandler {
	h := &CommitteeHandler{
		attDuties:  attDuties,
		syncDuties: syncDuties,

		validatorCommitteeIDs: make(map[phase0.ValidatorIndex]spectypes.CommitteeID),
	}

	return h
}

func (h *CommitteeHandler) Name() string {
	return "CLUSTER"
}

func (h *CommitteeHandler) HandleDuties(ctx context.Context) {
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
			epoch := h.network.Beacon.EstimatedEpochAtSlot(slot)
			period := h.network.Beacon.EstimatedSyncCommitteePeriodAtEpoch(epoch)
			buildStr := fmt.Sprintf("p%v-e%v-s%v-#%v", period, epoch, slot, slot%32+1)

			if !h.network.AlanForked(slot) {
				h.logger.Debug("🛠 ticker event",
					zap.String("period_epoch_slot_pos", buildStr),
					zap.String("status", "alan not forked yet"),
				)
				continue
			}

			h.logger.Debug("🛠 ticker event", zap.String("period_epoch_slot_pos", buildStr))
			h.processExecution(period, epoch, slot)

		case <-h.reorg:
			// do nothing

		case <-h.indicesChange:
			// do nothing
		}
	}
}

func (h *CommitteeHandler) processExecution(period uint64, epoch phase0.Epoch, slot phase0.Slot) {
	attDuties := h.attDuties.CommitteeSlotDuties(epoch, slot)
	syncDuties := h.syncDuties.CommitteePeriodDuties(period)
	if attDuties == nil && syncDuties == nil {
		return
	}

	committeeMap := h.buildCommitteeDuties(attDuties, syncDuties, epoch, slot)
	h.dutiesExecutor.ExecuteCommitteeDuties(h.logger, committeeMap)
}

func (h *CommitteeHandler) buildCommitteeDuties(attDuties []*eth2apiv1.AttesterDuty, syncDuties []*eth2apiv1.SyncCommitteeDuty, epoch phase0.Epoch, slot phase0.Slot) committeeDutiesMap {
	// NOTE: Instead of getting validators using duties one by one, we are getting all validators for the slot at once.
	// This approach reduces contention and improves performance, as multiple individual calls would be slower.
	vs := h.validatorProvider.SelfParticipatingValidators(epoch)
	for _, v := range vs {
		h.validatorCommitteeIDs[v.ValidatorIndex] = v.CommitteeID()
	}
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
	committeeID, ok := h.validatorCommitteeIDs[beaconDuty.ValidatorIndex]
	if !ok {
		h.logger.Error("can't find validator committeeID in validator store", zap.Uint64("validator_index", uint64(beaconDuty.ValidatorIndex)))
		return
	}

	if _, ok := m[committeeID]; !ok {
		m[committeeID] = &spectypes.CommitteeDuty{
			Slot:         beaconDuty.Slot,
			BeaconDuties: make([]*spectypes.BeaconDuty, 0),
		}
	}
	m[committeeID].BeaconDuties = append(m[committeeID].BeaconDuties, beaconDuty)
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
