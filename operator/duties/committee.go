package duties

import (
	"context"
	"fmt"
	"github.com/bloxapp/ssv/logging/fields"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/operator/duties/dutystore"
)

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

	start := time.Now()
	h.logger.Debug("building att committee duties", zap.Uint64("period", period), zap.Uint64("epoch", uint64(epoch)), fields.Slot(slot), zap.Int("duties", len(attDuties)))
	committeeMap := make(map[[32]byte]*spectypes.CommitteeDuty)
	if attDuties != nil {
		for i, d := range attDuties {
			if h.shouldExecuteAtt(d) {
				startv := time.Now()
				v := h.validatorProvider.Validator(d.PubKey[:])
				h.logger.Debug("time it takes to get validator", fields.Validator(d.PubKey[:]), zap.Duration("took", time.Since(startv)), zap.Int("index", i), fields.Slot(slot))
				startc := time.Now()
				clusterID := v.CommitteeID()
				h.logger.Debug("time it takes to make committeeID", fields.Validator(d.PubKey[:]), zap.Duration("took", time.Since(startc)), zap.Int("index", i), fields.Slot(slot))
				specDuty := h.toSpecAttDuty(d, spectypes.BNRoleAttester)

				if _, ok := committeeMap[clusterID]; !ok {
					committeeMap[clusterID] = &spectypes.CommitteeDuty{
						Slot:         specDuty.Slot,
						BeaconDuties: make([]*spectypes.BeaconDuty, 0),
					}
				}
				committeeMap[clusterID].BeaconDuties = append(committeeMap[clusterID].BeaconDuties, specDuty)
			}
		}
	}
	h.logger.Debug("Done building att committee duties", zap.Uint64("period", period), zap.Uint64("epoch", uint64(epoch)), zap.Uint64("slot", uint64(slot)), zap.Int("duties", len(attDuties)), zap.Duration("took", time.Since(start)))

	h.logger.Debug("building sc committee duties", zap.Uint64("period", period), zap.Uint64("epoch", uint64(epoch)), zap.Uint64("slot", uint64(slot)))
	if syncDuties != nil {
		for _, d := range syncDuties {
			if h.shouldExecuteSync(d, slot) {
				clusterID := h.validatorProvider.Validator(d.PubKey[:]).CommitteeID()
				specDuty := h.toSpecSyncDuty(d, slot, spectypes.BNRoleSyncCommittee)

				if _, ok := committeeMap[clusterID]; !ok {
					committeeMap[clusterID] = &spectypes.CommitteeDuty{
						Slot:         specDuty.Slot,
						BeaconDuties: make([]*spectypes.BeaconDuty, 0),
					}
				}
				committeeMap[clusterID].BeaconDuties = append(committeeMap[clusterID].BeaconDuties, specDuty)
			}
		}
	}
	h.logger.Debug("executing committee duties", zap.Uint64("period", period), zap.Uint64("epoch", uint64(epoch)), zap.Uint64("slot", uint64(slot)), zap.Int("duties", len(committeeMap)))
	h.executeCommitteeDuties(h.logger, committeeMap)
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

//// calculateSubscriptionInfo calculates the attester subscriptions given a set of duties.
//func calculateSubscriptionInfo(duties []*eth2apiv1.ClusterDuty) []*eth2apiv1.BeaconCommitteeSubscription {
//	subscriptions := make([]*eth2apiv1.BeaconCommitteeSubscription, 0, len(duties)*2)
//	for _, duty := range duties {
//		// Append a subscription for the attester role
//		subscriptions = append(subscriptions, toBeaconCommitteeSubscription(duty, spectypes.BNRoleCluster))
//		// Append a subscription for the aggregator role
//		subscriptions = append(subscriptions, toBeaconCommitteeSubscription(duty, spectypes.BNRoleAggregator))
//	}
//	return subscriptions
//}
//
//func toBeaconCommitteeSubscription(duty *eth2apiv1.ClusterDuty, role spectypes.BeaconRole) *eth2apiv1.BeaconCommitteeSubscription {
//	return &eth2apiv1.BeaconCommitteeSubscription{
//		ValidatorIndex:   duty.ValidatorIndex,
//		Slot:             duty.Slot,
//		CommitteeIndex:   duty.CommitteeIndex,
//		CommitteesAtSlot: duty.CommitteesAtSlot,
//		IsAggregator:     role == spectypes.BNRoleAggregator,
//	}
//}

func (h *CommitteeHandler) shouldFetchNexEpoch(slot phase0.Slot) bool {
	return uint64(slot)%h.network.Beacon.SlotsPerEpoch() > h.network.Beacon.SlotsPerEpoch()/2-2
}
