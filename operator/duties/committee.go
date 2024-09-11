package duties

import (
	"context"
	"sync"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
)

type validatorCommitteeDutyMap map[phase0.ValidatorIndex]*committeeDuty
type committeeDutiesMap map[spectypes.CommitteeID]*committeeDuty

type CommitteeHandler struct {
	baseHandler

	attHandler  *AttesterHandler
	syncHandler *SyncCommitteeHandler
}

type committeeDuty struct {
	duty        *spectypes.CommitteeDuty
	id          spectypes.CommitteeID
	operatorIDs []spectypes.OperatorID
}

func NewCommitteeHandler(attHandler *AttesterHandler, syncHandler *SyncCommitteeHandler) *CommitteeHandler {
	h := &CommitteeHandler{
		attHandler:  attHandler,
		syncHandler: syncHandler,
	}

	return h
}

func (h *CommitteeHandler) Name() string {
	return "COMMITTEE"
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
			tickerID := fields.FormatSlotTickerCommitteeID(period, epoch, slot)

			if !h.network.PastAlanForkAtEpoch(epoch) {
				h.logger.Debug("🛠 ticker event",
					fields.SlotTickerID(tickerID),
					zap.String("status", "alan not forked yet"),
				)
				continue
			}
			h.logger.Debug("🛠 ticker event", fields.SlotTickerID(tickerID))

			h.processExecution(period, epoch, slot)
			if h.indicesChanged {
				h.attHandler.duties.Reset(epoch)
				// we should not reset sync committee duties here as it is necessary for message validation
				// but this can lead to executing duties for deleted/liquidated validators
				h.indicesChanged = false
			}
			h.processFetching(ctx, period, epoch, slot)
			h.processSlotTransition(period, epoch, slot)

		case reorgEvent := <-h.reorg:
			epoch := h.network.Beacon.EstimatedEpochAtSlot(reorgEvent.Slot)
			period := h.network.Beacon.EstimatedSyncCommitteePeriodAtEpoch(epoch)
			tickerID := fields.FormatSlotTickerCommitteeID(period, epoch, reorgEvent.Slot)
			h.logger.Info("🔀 reorg event received", fields.SlotTickerID(tickerID), zap.Any("event", reorgEvent))

			h.attHandler.processReorg(ctx, epoch, reorgEvent)
			h.syncHandler.processReorg(period, reorgEvent)

		case <-h.indicesChange:
			slot := h.network.Beacon.EstimatedCurrentSlot()
			epoch := h.network.Beacon.EstimatedEpochAtSlot(slot)
			period := h.network.Beacon.EstimatedSyncCommitteePeriodAtEpoch(epoch)
			tickerID := fields.FormatSlotTickerCommitteeID(period, epoch, slot)
			h.logger.Info("🔁 indices change received", fields.SlotTickerID(tickerID))

			h.indicesChanged = true
			h.attHandler.processIndicesChange(epoch, slot)
			h.syncHandler.processIndicesChange(period, slot)
		}
	}
}

func (h *CommitteeHandler) processExecution(period uint64, epoch phase0.Epoch, slot phase0.Slot) {
	attDuties := h.attHandler.duties.CommitteeSlotDuties(epoch, slot)
	syncDuties := h.syncHandler.duties.CommitteePeriodDuties(period)
	if attDuties == nil && syncDuties == nil {
		return
	}

	committeeDuties := h.buildCommitteeDuties(attDuties, syncDuties, epoch, slot)
	h.dutiesExecutor.ExecuteCommitteeDuties(h.logger, committeeDuties)

	aggregationDuties := h.buildAggregationDuties(attDuties, syncDuties, slot)
	h.dutiesExecutor.ExecuteDuties(h.logger, aggregationDuties)

}

func (h *CommitteeHandler) processFetching(ctx context.Context, period uint64, epoch phase0.Epoch, slot phase0.Slot) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done() // Mark this goroutine as done once it completes
		h.attHandler.processFetching(ctx, epoch, slot)
	}()

	go func() {
		defer wg.Done() // Mark this goroutine as done once it completes
		h.syncHandler.processFetching(ctx, period, slot, true)
	}()

	wg.Wait()
}

func (h *CommitteeHandler) processSlotTransition(period uint64, epoch phase0.Epoch, slot phase0.Slot) {
	h.attHandler.processSlotTransition(epoch, slot)
	h.syncHandler.processSlotTransition(period, slot)
}

func (h *CommitteeHandler) buildCommitteeDuties(attDuties []*eth2apiv1.AttesterDuty, syncDuties []*eth2apiv1.SyncCommitteeDuty, epoch phase0.Epoch, slot phase0.Slot) committeeDutiesMap {
	// NOTE: Instead of getting validators using duties one by one, we are getting all validators for the slot at once.
	// This approach reduces contention and improves performance, as multiple individual calls would be slower.
	vs := h.validatorProvider.SelfParticipatingValidators(epoch)
	validatorCommitteeMap := make(validatorCommitteeDutyMap)
	committeeMap := make(committeeDutiesMap)
	for _, v := range vs {
		validatorCommitteeMap[v.ValidatorIndex] = &committeeDuty{
			id:          v.CommitteeID(),
			operatorIDs: v.OperatorIDs(),
		}
	}

	for _, d := range attDuties {
		if h.attHandler.shouldExecute(d) {
			specDuty := h.attHandler.toSpecDuty(d, spectypes.BNRoleAttester)
			h.appendBeaconDuty(validatorCommitteeMap, committeeMap, specDuty)
		}
	}

	for _, d := range syncDuties {
		if h.syncHandler.shouldExecute(d, slot) {
			specDuty := h.syncHandler.toSpecDuty(d, slot, spectypes.BNRoleSyncCommittee)
			h.appendBeaconDuty(validatorCommitteeMap, committeeMap, specDuty)
		}
	}

	return committeeMap
}

func (h *CommitteeHandler) buildAggregationDuties(attDuties []*eth2apiv1.AttesterDuty, syncDuties []*eth2apiv1.SyncCommitteeDuty, slot phase0.Slot) []*spectypes.ValidatorDuty {
	// aggregator and contribution duties
	toExecute := make([]*spectypes.ValidatorDuty, 0, len(attDuties)+len(syncDuties))
	for _, d := range attDuties {
		if h.attHandler.shouldExecute(d) {
			toExecute = append(toExecute, h.attHandler.toSpecDuty(d, spectypes.BNRoleAggregator))
		}
	}
	for _, d := range syncDuties {
		if h.syncHandler.shouldExecute(d, slot) {
			toExecute = append(toExecute, h.syncHandler.toSpecDuty(d, slot, spectypes.BNRoleSyncCommitteeContribution))
		}
	}

	return toExecute
}

func (h *CommitteeHandler) appendBeaconDuty(vc validatorCommitteeDutyMap, c committeeDutiesMap, beaconDuty *spectypes.ValidatorDuty) {
	if beaconDuty == nil {
		h.logger.Error("received nil beaconDuty")
		return
	}

	committee, ok := vc[beaconDuty.ValidatorIndex]
	if !ok {
		h.logger.Error("failed to find committee for validator", zap.Uint64("validator_index", uint64(beaconDuty.ValidatorIndex)))
		return
	}

	cd, ok := c[committee.id]
	if !ok {
		cd = &committeeDuty{
			id:          committee.id,
			operatorIDs: committee.operatorIDs,
			duty: &spectypes.CommitteeDuty{
				Slot:            beaconDuty.Slot,
				ValidatorDuties: make([]*spectypes.ValidatorDuty, 0),
			},
		}
		c[committee.id] = cd
	}

	cd.duty.ValidatorDuties = append(c[committee.id].duty.ValidatorDuties, beaconDuty)
}
