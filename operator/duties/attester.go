package duties

import (
	"context"
	"fmt"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/observability/traces"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/utils"
)

type AttesterHandler struct {
	baseHandler

	duties *dutystore.Duties[eth2apiv1.AttesterDuty]

	// dutyFetchIntents stores the unfulfilled intents to fetch duties for some target epochs.
	dutyFetchIntents map[phase0.Epoch]struct{}

	// lastTickedSlot keeps track of the last slot AttesterHandler processed.
	lastTickedSlot phase0.Slot

	exporterMode bool
}

func NewAttesterHandler(duties *dutystore.Duties[eth2apiv1.AttesterDuty], exporterMode bool) *AttesterHandler {
	h := &AttesterHandler{
		duties:           duties,
		exporterMode:     exporterMode,
		dutyFetchIntents: make(map[phase0.Epoch]struct{}),
	}
	return h
}

func (h *AttesterHandler) Name() string {
	return spectypes.BNRoleAttester.String()
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
//  2. EraseEpochData duties for the current epoch.
//  3. Fetch duties for the current epoch.
//  4. If necessary, fetch duties for the next epoch.
//
// On Ticker event:
//  1. Execute duties.
//  2. If necessary, fetch duties for the next epoch.
func (h *AttesterHandler) HandleDuties(ctx context.Context) {
	h.logger.Info("starting duty handler")
	defer h.logger.Info("duty handler exited")

	next := h.ticker.Next()
	for {
		select {
		case <-ctx.Done():
			return

		case <-next:
			// 1. Process the tick.

			currentSlot := h.ticker.Slot()
			next = h.ticker.Next() // advances h.ticker
			currentEpoch := h.beaconConfig.EstimatedEpochAtSlot(currentSlot)

			buildStr := fmt.Sprintf("e%v-s%v-#%v", currentEpoch, currentSlot, currentSlot%32+1)
			logger := h.logger.With(zap.String("epoch_slot_pos", buildStr))

			logger.Debug("🛠 ticker event", zap.String("epoch_slot_pos", buildStr))

			func() {
				tickCtx, cancel := h.ctxWithDeadlineInOneEpoch(ctx, currentSlot)
				defer cancel()

				h.executeAggregatorDuties(tickCtx, currentEpoch, currentSlot)

				h.prepareNextEpoch(ctx, currentEpoch, currentSlot)
			}()

			slotsPerEpoch := h.beaconConfig.SlotsPerEpoch

			// If we have reached the mid-point of the epoch, fetch the duties for the next epoch in the next slot.
			// This allows us to set them up at a time when the beacon node should be less busy.
			if uint64(currentSlot)%slotsPerEpoch == slotsPerEpoch/2-1 {
				h.dutyFetchIntents[currentEpoch+1] = struct{}{}
			}

			// Clean up the irrelevant data to prevent infinite memory growth.
			if uint64(currentSlot)%slotsPerEpoch == slotsPerEpoch-1 {
				h.duties.EraseEpochData(currentEpoch - 1)
				delete(h.dutyFetchIntents, currentEpoch-1)
			}

			h.lastTickedSlot = currentSlot

			// 2. Process validator indices changes (if any). We want to process it on the current slot only if we
			// are still early into the slot (1 slot-interval is just a guesstimate), otherwise we might be delaying
			// the next tick (the duties that need to be executed on the next slot).

			indicesChangeDeadline := h.beaconConfig.SlotStartTime(currentSlot).Add(h.beaconConfig.IntervalDuration())
			select {
			case <-h.indicesChange:
				logger.Info("🔁 indices change received")

				// Some validator-related state has updated, means we need to re-fetch the duties for the current
				// and next epoch to ensure we have the up-to-date duties for all validators for both epochs.
				h.dutyFetchIntents[currentEpoch] = struct{}{}
				h.dutyFetchIntents[currentEpoch+1] = struct{}{}

				// When at epoch boundary, we only care about pre-fetching & preparing the duties for the next epoch
				// (the current epoch will have been passed upon the next slot-tick). Otherwise, pre-fetch & prepare
				// the duties for the current epoch.
				if h.lastTickedSlotAtEpochBoundary() {
					h.prepareNextEpoch(ctx, currentEpoch, currentSlot)
				} else {
					h.prepareCurrentEpoch(ctx, currentEpoch, currentSlot)
				}
			case <-time.After(time.Until(indicesChangeDeadline)):
				// It's too late(risky) to handle indices change on the current slot, we'll do it on the next slot.
			}

		case reorgEvent := <-h.reorg:
			reorgEpoch := h.beaconConfig.EstimatedEpochAtSlot(reorgEvent.Slot)

			buildStr := fmt.Sprintf("e%v-s%v-#%v", reorgEpoch, reorgEvent.Slot, reorgEvent.Slot%32+1)
			logger := h.logger.With(zap.String("epoch_slot_pos", buildStr))

			logger.Info("🔀 reorg event received", zap.Any("event", reorgEvent))

			if !reorgEvent.Current {
				// Reorg on the previous epoch means the duties for the current epoch might have changed, so
				// we want to re-fetch them. We re-fetch immediately so that we have the correct duties to
				// execute on the next tick.
				h.dutyFetchIntents[reorgEpoch] = struct{}{}
			}

			// Reorg on the previous or current epoch means the duties for the next epoch might have changed, so
			// we want to re-fetch them.
			h.dutyFetchIntents[reorgEpoch+1] = struct{}{}

			// When at epoch boundary, we only care about pre-fetching & preparing the duties for the next epoch
			// (the current epoch will have been passed upon the next slot-tick). Otherwise, pre-fetch & prepare
			// the duties for the current epoch (but only if the reorg affects the current epoch).
			if h.lastTickedSlotAtEpochBoundary() {
				h.prepareNextEpoch(ctx, reorgEpoch, reorgEvent.Slot)
			} else if !reorgEvent.Current {
				h.prepareCurrentEpoch(ctx, reorgEpoch, reorgEvent.Slot)
			}
		}
	}
}

// HandleInitialDuties fetches & prepares the duties for the current and next epochs.
// Fetching duties for the next epoch is necessary if we are starting close to epoch-boundary because
// our ticker might "miss" that rollover otherwise.
func (h *AttesterHandler) HandleInitialDuties(ctx context.Context) {
	currentSlot := h.beaconConfig.EstimatedCurrentSlot()
	currentEpoch := h.beaconConfig.EstimatedEpochAtSlot(currentSlot)

	h.dutyFetchIntents[currentEpoch] = struct{}{}
	h.dutyFetchIntents[currentEpoch+1] = struct{}{}

	h.prepareCurrentEpoch(ctx, currentEpoch, currentSlot)
	h.prepareNextEpoch(ctx, currentEpoch, currentSlot)
}

// executeAggregatorDuties is only processing aggregator-duties after Alan fork.
func (h *AttesterHandler) executeAggregatorDuties(ctx context.Context, epoch phase0.Epoch, slot phase0.Slot) {
	if h.exporterMode {
		return
	}

	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "attester.execute"),
		trace.WithAttributes(
			observability.BeaconEpochAttribute(epoch),
			observability.BeaconSlotAttribute(slot),
			observability.BeaconRoleAttribute(spectypes.BNRoleAggregator),
		))
	defer span.End()

	duties := h.duties.CommitteeSlotDuties(epoch, slot)
	if duties == nil {
		span.AddEvent("no duties available")
		span.SetStatus(codes.Ok, "")
		return
	}

	span.AddEvent("duties fetched", trace.WithAttributes(observability.DutyCountAttribute(len(duties))))
	toExecute := make([]*spectypes.ValidatorDuty, 0, len(duties))
	for _, d := range duties {
		if h.shouldExecute(d) {
			// For every attestation duty we also have to try to perform aggregation duty even if it
			// isn't necessarily needed - we won't know if it's needed or not until we rebuild
			// validator signature (done during pre-consensus step) and perform some computation on
			// it - hence scheduling it for execution here.
			toExecute = append(toExecute, h.toSpecDuty(d, spectypes.BNRoleAggregator))
		}
	}

	span.AddEvent("executing duties", trace.WithAttributes(observability.DutyCountAttribute(len(toExecute))))

	h.dutiesExecutor.ExecuteDuties(ctx, toExecute)

	span.SetStatus(codes.Ok, "")
}

func (h *AttesterHandler) prepareCurrentEpoch(ctx context.Context, currentEpoch phase0.Epoch, currentSlot phase0.Slot) {
	if _, ok := h.dutyFetchIntents[currentEpoch]; ok {
		err := h.fetchAndProcessDuties(ctx, currentEpoch, currentSlot)
		if err != nil {
			h.logger.Error("failed to prepare duties for current epoch", zap.Error(err))
			return
		}
		delete(h.dutyFetchIntents, currentEpoch) // the intent has been fulfilled
	}
}

func (h *AttesterHandler) prepareNextEpoch(ctx context.Context, currentEpoch phase0.Epoch, currentSlot phase0.Slot) {
	if _, ok := h.dutyFetchIntents[currentEpoch+1]; ok && h.goodTimeToFetchDutiesForNextEpoch(currentSlot) {
		err := h.fetchAndProcessDuties(ctx, currentEpoch+1, currentSlot)
		if err != nil {
			h.logger.Error("failed to prepare duties for next epoch", zap.Error(err))
			return
		}
		delete(h.dutyFetchIntents, currentEpoch+1) // the intent has been fulfilled
	}
}

func (h *AttesterHandler) fetchAndProcessDuties(ctx context.Context, epoch phase0.Epoch, slot phase0.Slot) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "attester.fetch_and_store"),
		trace.WithAttributes(
			observability.BeaconEpochAttribute(epoch),
			observability.BeaconSlotAttribute(slot),
			observability.BeaconRoleAttribute(spectypes.BNRoleAttester),
		))
	defer span.End()

	start := time.Now()

	var eligibleShares []*types.SSVShare
	for _, share := range h.validatorProvider.SelfValidators() {
		if share.IsAttesting(epoch) {
			eligibleShares = append(eligibleShares, share)
		}
	}

	eligibleIndices := indicesFromShares(eligibleShares)
	if len(eligibleIndices) == 0 {
		const eventMsg = "no active validators for epoch"
		h.logger.Debug(eventMsg, fields.Epoch(epoch))
		span.AddEvent(eventMsg)
		span.SetStatus(codes.Ok, "")
		return nil
	}

	span.AddEvent("fetching duties from beacon node", trace.WithAttributes(observability.ValidatorCountAttribute(len(eligibleIndices))))
	duties, err := h.beaconNode.AttesterDuties(ctx, epoch, eligibleIndices)
	if err != nil {
		return traces.Errorf(span, "failed to fetch attester duties: %w", err)
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
		span.AddEvent("will store duty", trace.WithAttributes(observability.ValidatorIndexAttribute(d.ValidatorIndex)))
		specDuties = append(specDuties, h.toSpecDuty(d, spectypes.BNRoleAttester))
	}

	span.AddEvent("storing duties", trace.WithAttributes(observability.DutyCountAttribute(len(storeDuties))))
	h.duties.Set(epoch, storeDuties)

	truncate := -1
	if h.exporterMode {
		truncate = 10
	}
	h.logger.Debug("🗂 got duties",
		fields.Count(len(duties)),
		fields.Epoch(epoch),
		fields.Duties(epoch, specDuties, truncate),
		fields.Took(time.Since(start)),
	)

	// Further processing is not needed in exporter mode, terminate early
	// avoiding CL subscriptions saves some CPU & Network resources
	// and avoids unnecessary log noise
	if h.exporterMode {
		span.SetStatus(codes.Ok, "")
		return nil
	}

	// calculate subscriptions
	subscriptions := calculateSubscriptionInfo(duties, slot)
	if len(subscriptions) == 0 {
		span.AddEvent("no subscriptions available")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	span.AddEvent("submitting beacon committee subscriptions", trace.WithAttributes(
		attribute.Int("ssv.validator.duty.subscriptions", len(subscriptions)),
	))

	go func() {
		// Cannot use parent-context itself here, have to create independent instance
		// to be able to continue working in background.
		subscriptionCtx, cancel, withDeadline := utils.CtxWithParentDeadline(ctx)
		defer cancel()
		if !withDeadline {
			h.logger.Warn("parent-context has no deadline set")
		}

		if err := h.beaconNode.SubmitBeaconCommitteeSubscriptions(subscriptionCtx, subscriptions); err != nil {
			h.logger.Error("failed to submit beacon committee subscription", zap.Error(err))
		}
	}()

	span.SetStatus(codes.Ok, "")
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

func (h *AttesterHandler) shouldExecute(duty *eth2apiv1.AttesterDuty) bool {
	currentSlot := h.beaconConfig.EstimatedCurrentSlot()
	currentEpoch := h.beaconConfig.EstimatedEpochAtSlot(currentSlot)

	v, exists := h.validatorProvider.Validator(duty.PubKey[:])
	if !exists {
		h.logger.Warn("validator not found", fields.Validator(duty.PubKey[:]))
		return false
	}

	if v.MinParticipationEpoch() > currentEpoch {
		h.logger.Debug("validator not yet participating",
			fields.Validator(duty.PubKey[:]),
			zap.Uint64("min_participation_epoch", uint64(v.MinParticipationEpoch())),
			zap.Uint64("current_epoch", uint64(currentEpoch)),
		)
		return false
	}

	// execute task if slot already began and not pass 1 epoch
	maxAttestationPropagationDelay := h.beaconConfig.SlotsPerEpoch
	if currentSlot >= duty.Slot && uint64(currentSlot-duty.Slot) <= maxAttestationPropagationDelay {
		return true
	}
	if currentSlot+1 == duty.Slot {
		h.warnMisalignedSlotAndDuty(duty.String())
		return true
	}
	return false
}

// calculateSubscriptionInfo calculates the attester subscriptions given a set of duties.
func calculateSubscriptionInfo(duties []*eth2apiv1.AttesterDuty, slot phase0.Slot) []*eth2apiv1.BeaconCommitteeSubscription {
	subscriptions := make([]*eth2apiv1.BeaconCommitteeSubscription, 0, len(duties)*2)
	for _, duty := range duties {
		if duty.Slot < slot {
			continue
		}
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

func (h *AttesterHandler) goodTimeToFetchDutiesForNextEpoch(slot phase0.Slot) bool {
	slotsPerEpoch := h.beaconConfig.SlotsPerEpoch
	return uint64(slot)%slotsPerEpoch > slotsPerEpoch/2-2
}

func (h *AttesterHandler) lastTickedSlotAtEpochBoundary() bool {
	slotsPerEpoch := h.beaconConfig.SlotsPerEpoch
	return uint64(h.lastTickedSlot+1)%slotsPerEpoch == 0
}
