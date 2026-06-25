package duties

import (
	"context"
	"fmt"
	"sync"
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
)

type AttesterHandler struct {
	baseHandler

	duties *dutystore.Duties[eth2apiv1.AttesterDuty]

	// dutyFetchIntents stores the intents to fetch duties for some target epochs, the bool indicates whether the
	// intent has already been fulfilled.
	dutyFetchIntents map[phase0.Epoch]bool

	// backgroundTasks tracks all go-routines spawned by Scheduler for graceful shutdown.
	backgroundTasks sync.WaitGroup

	exporterMode bool
}

func NewAttesterHandler(duties *dutystore.Duties[eth2apiv1.AttesterDuty], exporterMode bool) *AttesterHandler {
	h := &AttesterHandler{
		duties:           duties,
		exporterMode:     exporterMode,
		dutyFetchIntents: make(map[phase0.Epoch]bool),
	}
	return h
}

func (h *AttesterHandler) Name() string {
	return spectypes.BNRoleAttester.String()
}

func (h *AttesterHandler) WaitShutdown() {
	h.backgroundTasks.Wait()
}

// HandleDuties manages the duty lifecycle, handling different cases:
//
// On First Run:
//  1. If necessary, fetch duties for the current epoch.
//  2. If necessary, fetch duties for the next epoch.
//  3. Duties will be executed on the very next slot-tick.
//
// On Re-org:
//  1. Declare intents to fetch duties for the epochs affected by the reorg (depending on whether the
//     previous or current dependent root changed).
//  2. If necessary, fetch duties for the current/next epochs so they can be processed on the next slot-tick.
//  3. Duties will be executed on the very next slot-tick.
//
// On Ticker event:
//  1. If necessary, fetch duties for the current epoch.
//  2. Execute duties.
//  3. If necessary, fetch duties for the next epoch.
//  4. If necessary, process validator-indices changes by declaring the intents to fetch duties for the epochs
//     affected by it, also potentially pre-fetching duties so they are ready for processing on the next slot-tick.
func (h *AttesterHandler) HandleDuties(ctx context.Context) {
	h.logger.Info("starting duty handler")
	defer h.logger.Info("duty handler exited")

	next := h.ticker.Next()
	for {
		select {
		case <-ctx.Done():
			return

		case <-next:
			currentSlot := h.ticker.Slot()
			next = h.ticker.Next() // advances h.ticker
			currentEpoch := h.netCfg.EstimatedEpochAtSlot(currentSlot)
			nextEpoch := currentEpoch + 1

			slotNumber := uint64(currentSlot)%h.netCfg.SlotsPerEpoch + 1
			buildStr := fmt.Sprintf("e%v-s%v-#%v", currentEpoch, currentSlot, slotNumber)
			logger := h.logger.With(
				zap.String("epoch_slot_pos", buildStr),
				zap.Uint64("current_epoch", uint64(currentEpoch)),
				zap.Uint64("current_slot", uint64(currentSlot)),
			)

			logger.Debug("🛠 ticker event")

			func() {
				// tickCtx ensures we never take too long to process ticks (otherwise we might not be able to catch up
				// with the latest tick for a while, if ever). Since the ticker always fires at around slot start-time,
				// setting the deadline to currentSlot+1 gives us about ~1 full slot (12s) to process the tick.
				tickCtx, cancel := context.WithDeadline(ctx, h.netCfg.SlotStartTime(currentSlot+1))
				defer cancel()

				// 1. Process the duty execution & fetching.

				// Process intents (if any): fetch & prepare the duties for the current epoch.
				h.prepareCurrentEpoch(tickCtx, logger, currentEpoch, currentSlot)

				if !h.netCfg.BooleForkAtSlot(currentSlot) {
					h.executeAggregatorDuties(tickCtx, currentEpoch, currentSlot)
				}

				// Process intents (if any): fetch & prepare the duties for the next epoch.
				h.prepareNextEpoch(tickCtx, logger, currentEpoch, currentSlot)

				// Clean up the irrelevant data to prevent infinite memory growth at the very 1st slot of the epoch.
				// Note, it doesn't have to be "the very 1st slot" exactly - it's just the most natural time to do it.
				if slotNumber == 1 && currentEpoch >= 1 {
					h.duties.EraseEpochData(currentEpoch - 1)
					delete(h.dutyFetchIntents, currentEpoch-1)
				}

				// 2. Schedule the next-epoch duty-fetch intent (only if absent, so a pending or fulfilled one is
				// left untouched). We register it before the indices-change handling below, which can return early
				// on tickCtx.Done - so even a tick that overruns its deadline still records the intent, and the
				// next-epoch pre-fetch can't be deferred indefinitely at an epoch boundary under sustained slowness.
				if _, ok := h.dutyFetchIntents[nextEpoch]; !ok {
					h.dutyFetchIntents[nextEpoch] = false
				}

				// 3. Process validator indices changes (if any). We want to process it on the current slot only
				// if we are still early into the slot (1 slot-interval is just a guesstimate), otherwise we might
				// be delaying the next tick (the duties that need to be executed on the next slot).

				indicesChangeDeadline := h.netCfg.SlotStartTime(currentSlot).Add(h.netCfg.IntervalDuration(currentSlot))
				select {
				case <-h.indicesChangeCh:
					logger.Info("🔁 indices change received")

					// 1) Declare intents.
					// Some validator-related state has changed, so re-fetch the duties for the current and next
					// epoch to keep them up to date for all validators.
					h.dutyFetchIntents[currentEpoch] = false
					h.dutyFetchIntents[nextEpoch] = false

					// 2) Process certain intents immediately.
					// When at epoch boundary, we only care about pre-fetching & preparing the duties for the next
					// epoch (the current epoch will have been passed upon the next slot-tick). Otherwise, pre-fetch &
					// prepare the duties for the current epoch.
					if h.atLastSlotOfCurrentEpoch(currentSlot) {
						delete(h.dutyFetchIntents, currentEpoch) // optimization: prune irrelevant intent
						h.prepareNextEpoch(tickCtx, logger, currentEpoch, currentSlot)
					} else {
						h.prepareCurrentEpoch(tickCtx, logger, currentEpoch, currentSlot)
					}
				case <-time.After(time.Until(indicesChangeDeadline)):
					// It's too late(risky) to handle indices change on the current slot, we'll do it on the next slot.
				case <-tickCtx.Done():
					return
				}
			}()

		case reorgEvent := <-h.reorgEventsCh:
			currentSlot := h.netCfg.EstimatedCurrentSlot()
			currentEpoch := h.netCfg.EstimatedEpochAtSlot(currentSlot)
			nextEpoch := currentEpoch + 1

			slotNumber := uint64(currentSlot)%h.netCfg.SlotsPerEpoch + 1
			buildStr := fmt.Sprintf("e%v-s%v-#%v", currentEpoch, currentSlot, slotNumber)

			logger := h.logger.With(
				zap.String("epoch_slot_pos", buildStr),
				zap.Uint64("current_epoch", uint64(currentEpoch)),
				zap.Uint64("current_slot", uint64(currentSlot)),
			)

			// Attester duties for the current epoch are determined by the "previous duty dependent root",
			// so we re-fetch the current epoch only if it has changed. The next epoch is always re-fetched
			// on any reorg to ensure we have the up-to-date duties for all validators.
			refetchCurrentEpoch := reorgEvent.PreviousDutyDependentRootChanged

			logger.Info("🔀 reorg event received",
				zap.Any("event", reorgEvent),
				zap.Bool("refetch_current_epoch_duties", refetchCurrentEpoch),
			)

			func() {
				// reorgCtx ensures we never take too long to process the reorg (we don't want to prevent the
				// slot-ticker from executing duties even if some of them might not be up to date). Since the
				// reorg can happen closer to the end of the current slot we wouldn't want to set the deadline
				// to currentSlot+1 as that might be too short (hence setting it to currentSlot+2).
				reorgCtx, cancel := context.WithDeadline(ctx, h.netCfg.SlotStartTime(currentSlot+2))
				defer cancel()

				// 1) Declare intents.
				if refetchCurrentEpoch {
					h.dutyFetchIntents[currentEpoch] = false
				}
				h.dutyFetchIntents[nextEpoch] = false

				// 2) Process certain intents immediately.
				// When at epoch boundary, we only care about pre-fetching & preparing the duties for the next epoch
				// since the current epoch will have been passed upon the next slot-tick. Otherwise, we might need to
				// pre-fetch & prepare the duties for the current epoch immediately since those might have been
				// affected by this reorg (the next tick(s) will take care of the pre-fetch & prepare for the next
				// epoch, if it was also affected by this reorg).
				if h.atLastSlotOfCurrentEpoch(currentSlot) {
					delete(h.dutyFetchIntents, currentEpoch) // optimization: prune irrelevant intent
					h.prepareNextEpoch(reorgCtx, logger, currentEpoch, currentSlot)
				} else {
					h.prepareCurrentEpoch(reorgCtx, logger, currentEpoch, currentSlot)
				}
			}()
		}
	}
}

// HandleInitialDuties fetches & prepares the duties for the current and next epochs.
func (h *AttesterHandler) HandleInitialDuties(ctx context.Context) {
	// initCtx ensures we don't block indefinitely in case we can't fetch the duties on startup.
	initCtx, cancel := context.WithTimeout(ctx, h.netCfg.SlotDuration)
	defer cancel()

	currentSlot := h.netCfg.EstimatedCurrentSlot()
	currentEpoch := h.netCfg.EstimatedEpochAtSlot(currentSlot)
	nextEpoch := currentEpoch + 1

	slotNumber := uint64(currentSlot)%h.netCfg.SlotsPerEpoch + 1
	buildStr := fmt.Sprintf("e%v-s%v-#%v", currentEpoch, currentSlot, slotNumber)
	logger := h.logger.With(
		zap.String("epoch_slot_pos", buildStr),
		zap.Uint64("current_epoch", uint64(currentEpoch)),
		zap.Uint64("current_slot", uint64(currentSlot)),
	)

	// 1) Declare intents.
	h.dutyFetchIntents[currentEpoch] = false
	h.dutyFetchIntents[nextEpoch] = false

	// 2) Process certain intents immediately.
	// At the last slot of current epoch we don't fetch duties for the current epoch because we likely won't
	// have enough time to process those duties anyway ... but we do want to fetch the duties for the next epoch
	// right away in that case since we'll need to be able to execute those duties on the next tick - the tick
	// corresponding to the 1st slot of the next epoch.
	if h.atLastSlotOfCurrentEpoch(currentSlot) {
		delete(h.dutyFetchIntents, currentEpoch) // optimization: prune irrelevant intent
		h.prepareNextEpoch(initCtx, logger, currentEpoch, currentSlot)
	} else {
		h.prepareCurrentEpoch(initCtx, logger, currentEpoch, currentSlot)
	}
}

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

	// Attestation and aggregation submissions are rewarded as long as they are included within
	// SLOTS_PER_EPOCH slots of their target slot (i.e., from target slot up to and including target + SLOTS_PER_EPOCH).
	// See https://eth2book.info/latest/part2/incentives/rewards/#attestation-rewards
	// Sync committee duties have to use the same deadline because they are part of the committee role.
	// We set the deadline to target slot + SLOTS_PER_EPOCH + 1 (since the deadline slot itself is excluded).
	slotsPerEpoch := phase0.Slot(h.netCfg.SlotsPerEpoch)
	dutyDeadline := h.netCfg.SlotStartTime(slot + slotsPerEpoch + 1)
	h.dutiesExecutor.ExecuteDuties(ctx, toExecute, dutyDeadline)

	span.SetStatus(codes.Ok, "")
}

func (h *AttesterHandler) prepareCurrentEpoch(ctx context.Context, logger *zap.Logger, currentEpoch phase0.Epoch, currentSlot phase0.Slot) {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "attester.prepare_current_epoch"),
		trace.WithAttributes(
			observability.BeaconEpochAttribute(currentEpoch),
			observability.BeaconSlotAttribute(currentSlot),
			observability.BeaconRoleAttribute(spectypes.BNRoleAttester),
		))
	defer span.End()

	if fulfilled, ok := h.dutyFetchIntents[currentEpoch]; ok && !fulfilled {
		logger.Debug("fetching duties for the current epoch")

		err := h.fetchAndProcessDuties(ctx, logger, currentEpoch, currentSlot)
		if err != nil {
			logger.Error("fetching duties for the current epoch failed", zap.Error(err))
			span.SetStatus(codes.Error, err.Error())
			return
		}
		h.dutyFetchIntents[currentEpoch] = true // the intent has been fulfilled

		logger.Debug("fetching duties for the current epoch succeeded")
	}

	span.SetStatus(codes.Ok, "")
}

func (h *AttesterHandler) prepareNextEpoch(ctx context.Context, logger *zap.Logger, currentEpoch phase0.Epoch, currentSlot phase0.Slot) {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "attester.prepare_next_epoch"),
		trace.WithAttributes(
			observability.BeaconEpochAttribute(currentEpoch+1),
			observability.BeaconSlotAttribute(currentSlot),
			observability.BeaconRoleAttribute(spectypes.BNRoleAttester),
		))
	defer span.End()

	// Delaying the duty fetch until it's a "good time" allows us to do it when the beacon node should be less busy.
	if fulfilled, ok := h.dutyFetchIntents[currentEpoch+1]; ok && !fulfilled && h.shouldFetchNextEpoch(currentSlot) {
		logger.Debug("fetching duties for the next epoch")

		err := h.fetchAndProcessDuties(ctx, logger, currentEpoch+1, currentSlot)
		if err != nil {
			logger.Error("fetching duties for the next epoch failed", zap.Error(err))
			span.SetStatus(codes.Error, err.Error())
			return
		}
		h.dutyFetchIntents[currentEpoch+1] = true // the intent has been fulfilled

		logger.Debug("fetching duties for the next epoch succeeded")
	}

	span.SetStatus(codes.Ok, "")
}

func (h *AttesterHandler) fetchAndProcessDuties(ctx context.Context, logger *zap.Logger, targetEpoch phase0.Epoch, currentSlot phase0.Slot) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "attester.fetch_and_store"),
		trace.WithAttributes(
			observability.BeaconEpochAttribute(targetEpoch),
			observability.BeaconSlotAttribute(currentSlot),
			observability.BeaconRoleAttribute(spectypes.BNRoleAttester),
		))
	defer span.End()

	logger = logger.With(zap.Uint64("target_epoch", uint64(targetEpoch)))

	start := time.Now()

	var eligibleShares []*types.SSVShare
	for _, share := range h.validatorProvider.SelfValidators() {
		if share.IsAttesting(targetEpoch) {
			eligibleShares = append(eligibleShares, share)
		}
	}

	eligibleIndices := indicesFromShares(eligibleShares)
	if len(eligibleIndices) == 0 {
		const eventMsg = "no active validators for epoch"
		logger.Debug(eventMsg)
		span.AddEvent(eventMsg)
		span.SetStatus(codes.Ok, "")
		return nil
	}

	span.AddEvent("fetching duties from beacon node", trace.WithAttributes(observability.ValidatorCountAttribute(len(eligibleIndices))))
	duties, err := h.beaconNode.AttesterDuties(ctx, targetEpoch, eligibleIndices)
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
	h.duties.Set(targetEpoch, storeDuties)

	truncate := -1
	if h.exporterMode {
		truncate = 10
	}
	logger.Debug("🗂 got duties",
		fields.Count(len(duties)),
		fields.Duties(targetEpoch, specDuties, truncate, func(duty *spectypes.ValidatorDuty) spectypes.RunnerRole {
			return types.RunnerRoleForValidatorDuty(duty, h.netCfg.BooleForkAtSlot(duty.Slot))
		}),
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
	subscriptions := calculateSubscriptionInfo(duties, currentSlot)
	if len(subscriptions) == 0 {
		span.AddEvent("no subscriptions available")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	span.AddEvent("submitting beacon committee subscriptions", trace.WithAttributes(
		attribute.Int("ssv.validator.duty.subscriptions", len(subscriptions)),
	))

	h.backgroundTasks.Add(1)
	go func() {
		defer h.backgroundTasks.Done()

		// Cannot use parent-context itself here, have to create independent instance
		// to be able to continue working in background.
		subscriptionCtx, cancel := context.WithCancel(h.ctx)
		defer cancel()

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
	currentSlot := h.netCfg.EstimatedCurrentSlot()
	currentEpoch := h.netCfg.EstimatedEpochAtSlot(currentSlot)

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
	maxAttestationPropagationDelay := h.netCfg.SlotsPerEpoch
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
