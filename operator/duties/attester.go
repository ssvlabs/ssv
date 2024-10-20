package duties

import (
	"context"
	"fmt"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
)

type AttesterHandler struct {
	baseHandler

	duties            *dutystore.Duties[eth2apiv1.AttesterDuty]
	fetchCurrentEpoch bool
	fetchNextEpoch    bool

	firstRun bool
}

func NewAttesterHandler(duties *dutystore.Duties[eth2apiv1.AttesterDuty]) *AttesterHandler {
	h := &AttesterHandler{
		duties:   duties,
		firstRun: true,
	}
	return h
}

func (h *AttesterHandler) Name() string {
	return spectypes.BNRoleAttester.String()
}

// HandleDuties manages the duty lifecycle
func (h *AttesterHandler) HandleDuties(ctx context.Context) {
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
			tickerID := fields.FormatSlotTickerID(epoch, slot)
			h.logger.Debug("🛠 ticker event", fields.SlotTickerID(tickerID))

			if !h.network.PastAlanForkAtEpoch(epoch) {
				if h.firstRun {
					h.processFirstRun(ctx, epoch, slot)
				}
				h.processExecution(epoch, slot)
				if h.indicesChanged {
					h.processIndicesChange(epoch, slot)
				}
				h.processFetching(ctx, epoch, slot)
				h.processSlotTransition(epoch, slot)
			}

		case reorgEvent := <-h.reorg:
			epoch := h.network.Beacon.EstimatedEpochAtSlot(reorgEvent.Slot)
			tickerID := fields.FormatSlotTickerID(epoch, reorgEvent.Slot)
			h.logger.Info("🔀 reorg event received", fields.SlotTickerID(tickerID), zap.Any("event", reorgEvent))

			if !h.network.PastAlanForkAtEpoch(epoch) {
				h.processReorg(ctx, epoch, reorgEvent)
			}

		case <-h.indicesChange:
			slot := h.network.Beacon.EstimatedCurrentSlot()
			epoch := h.network.Beacon.EstimatedEpochAtSlot(slot)
			tickerID := fields.FormatSlotTickerID(epoch, slot)
			h.logger.Info("🔁 indices change received", fields.SlotTickerID(tickerID))

			if !h.network.PastAlanForkAtEpoch(epoch) {
				h.indicesChanged = true
			}
		}
	}
}

func (h *AttesterHandler) processFirstRun(ctx context.Context, epoch phase0.Epoch, slot phase0.Slot) {
	h.fetchCurrentEpoch = true
	h.processFetching(ctx, epoch, slot)

	if uint64(slot)%h.network.Beacon.SlotsPerEpoch() > h.network.Beacon.SlotsPerEpoch()/2-1 {
		h.fetchNextEpoch = true
	}
	h.firstRun = false
}

func (h *AttesterHandler) processFetching(ctx context.Context, epoch phase0.Epoch, slot phase0.Slot) {
	ctx, cancel := context.WithDeadline(ctx, h.network.Beacon.GetSlotStartTime(slot+1).Add(100*time.Millisecond))
	defer cancel()

	if h.fetchCurrentEpoch {
		if err := h.fetchAndProcessDuties(ctx, epoch); err != nil {
			h.logger.Error("failed to fetch duties for current epoch", zap.Error(err))
			return
		}
		h.fetchCurrentEpoch = false
	}

	if h.fetchNextEpoch {
		if err := h.fetchAndProcessDuties(ctx, epoch+1); err != nil {
			h.logger.Error("failed to fetch duties for next epoch", zap.Error(err))
			return
		}
		h.fetchNextEpoch = false
	}
}

func (h *AttesterHandler) processExecution(epoch phase0.Epoch, slot phase0.Slot) {
	duties := h.duties.CommitteeSlotDuties(epoch, slot)
	if duties == nil {
		return
	}

	toExecute := make([]*genesisspectypes.Duty, 0, len(duties)*2)
	for _, d := range duties {
		if h.shouldExecute(d) {
			toExecute = append(toExecute, h.toGenesisSpecDuty(d, genesisspectypes.BNRoleAttester))
			toExecute = append(toExecute, h.toGenesisSpecDuty(d, genesisspectypes.BNRoleAggregator))
		}
	}

	h.dutiesExecutor.ExecuteGenesisDuties(h.logger, toExecute)
}

func (h *AttesterHandler) processIndicesChange(epoch phase0.Epoch, slot phase0.Slot) {
	h.duties.Reset(epoch)
	h.fetchCurrentEpoch = true

	// reset next epoch duties if in appropriate slot range
	if h.shouldFetchNexEpoch(slot) {
		h.duties.Reset(epoch + 1)
		h.fetchNextEpoch = true
	}

	h.indicesChanged = false
}

func (h *AttesterHandler) processReorg(ctx context.Context, epoch phase0.Epoch, reorgEvent ReorgEvent) {
	// reset current epoch duties
	if reorgEvent.Previous {
		h.duties.Reset(epoch)
		h.fetchCurrentEpoch = true
		if h.shouldFetchNexEpoch(reorgEvent.Slot) {
			h.duties.Reset(epoch + 1)
			h.fetchNextEpoch = true
		}

		h.processFetching(ctx, epoch, reorgEvent.Slot)
	} else if reorgEvent.Current {
		// reset & re-fetch next epoch duties if in appropriate slot range,
		// otherwise they will be fetched by the appropriate slot tick.
		if h.shouldFetchNexEpoch(reorgEvent.Slot) {
			h.duties.Reset(epoch + 1)
			h.fetchNextEpoch = true
		}
	}
}

func (h *AttesterHandler) processSlotTransition(epoch phase0.Epoch, slot phase0.Slot) {
	slotsPerEpoch := h.network.Beacon.SlotsPerEpoch()

	// If we have reached the mid-point of the epoch, fetch the duties for the next epoch in the next slot.
	// This allows us to set them up at a time when the beacon node should be less busy.
	if uint64(slot)%slotsPerEpoch == slotsPerEpoch/2-1 {
		h.fetchNextEpoch = true
	}

	// last slot of epoch
	if uint64(slot)%slotsPerEpoch == slotsPerEpoch-1 {
		h.duties.Reset(epoch - 1)
	}
}

func (h *AttesterHandler) fetchAndProcessDuties(ctx context.Context, epoch phase0.Epoch) error {
	start := time.Now()
	indices := indicesFromShares(h.validatorProvider.SelfParticipatingValidators(epoch))

	if len(indices) == 0 {
		h.logger.Debug("no active validators for epoch", fields.Epoch(epoch))
		return nil
	}

	duties, err := h.beaconNode.AttesterDuties(ctx, epoch, indices)
	if err != nil {
		return fmt.Errorf("failed to fetch attester duties: %w", err)
	}

	specDuties := make([]*spectypes.ValidatorDuty, 0, len(duties))
	storeDuties := make([]dutystore.StoreDuty[eth2apiv1.AttesterDuty], 0, len(duties))
	for _, d := range duties {
		storeDuties = append(storeDuties, dutystore.StoreDuty[eth2apiv1.AttesterDuty]{
			Slot:           d.Slot,
			ValidatorIndex: d.ValidatorIndex,
			Duty:           d,
			InCommittee:    true,
		})
		specDuties = append(specDuties, h.toSpecDuty(d, spectypes.BNRoleAttester))
	}
	h.duties.Set(epoch, storeDuties)

	h.logger.Debug("🗂 got duties",
		fields.Count(len(duties)),
		fields.Epoch(epoch),
		fields.Duties(epoch, specDuties),
		fields.Duration(start))

	// calculate subscriptions
	subscriptions := calculateSubscriptionInfo(duties)
	if len(subscriptions) > 0 {
		if deadline, ok := ctx.Deadline(); ok {
			go func(h *AttesterHandler, subscriptions []*eth2apiv1.BeaconCommitteeSubscription) {
				// Create a new subscription context with a deadline from parent context.
				subscriptionCtx, cancel := context.WithDeadline(context.Background(), deadline)
				defer cancel()
				if err := h.beaconNode.SubmitBeaconCommitteeSubscriptions(subscriptionCtx, subscriptions); err != nil {
					h.logger.Warn("failed to submit beacon committee subscription", zap.Error(err))
				}
			}(h, subscriptions)
		} else {
			h.logger.Warn("failed to get context deadline")
		}
	}

	return nil
}

func (h *AttesterHandler) toSpecDuty(duty *eth2apiv1.AttesterDuty, role spectypes.BeaconRole) *spectypes.ValidatorDuty {
	return &spectypes.ValidatorDuty{
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

func (h *AttesterHandler) toGenesisSpecDuty(duty *eth2apiv1.AttesterDuty, role genesisspectypes.BeaconRole) *genesisspectypes.Duty {
	return &genesisspectypes.Duty{
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

func (h *AttesterHandler) shouldExecute(duty *eth2apiv1.AttesterDuty) bool {
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

// calculateSubscriptionInfo calculates the attester subscriptions given a set of duties.
func calculateSubscriptionInfo(duties []*eth2apiv1.AttesterDuty) []*eth2apiv1.BeaconCommitteeSubscription {
	subscriptions := make([]*eth2apiv1.BeaconCommitteeSubscription, 0, len(duties)*2)
	for _, duty := range duties {
		// Append a subscription for the attester role
		subscriptions = append(subscriptions, toBeaconCommitteeSubscription(duty, spectypes.BNRoleAttester))
		// Append a subscription for the aggregator role
		subscriptions = append(subscriptions, toBeaconCommitteeSubscription(duty, spectypes.BNRoleAggregator))
	}
	return subscriptions
}

func toBeaconCommitteeSubscription(duty *eth2apiv1.AttesterDuty, role spectypes.BeaconRole) *eth2apiv1.BeaconCommitteeSubscription {
	return &eth2apiv1.BeaconCommitteeSubscription{
		ValidatorIndex:   duty.ValidatorIndex,
		Slot:             duty.Slot,
		CommitteeIndex:   duty.CommitteeIndex,
		CommitteesAtSlot: duty.CommitteesAtSlot,
		IsAggregator:     role == spectypes.BNRoleAggregator,
	}
}

func (h *AttesterHandler) shouldFetchNexEpoch(slot phase0.Slot) bool {
	return uint64(slot)%h.network.Beacon.SlotsPerEpoch() >= h.network.Beacon.SlotsPerEpoch()/2-1
}
