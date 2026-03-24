package duties

import (
	"context"
	"fmt"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/observability/traces"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
)

type ProposerHandler struct {
	baseHandler

	duties *dutystore.Duties[eth2apiv1.ProposerDuty]

	// dutyFetchIntents stores the intents to fetch duties for some target epochs, the bool indicates whether the
	// intent has already been fulfilled.
	dutyFetchIntents map[phase0.Epoch]bool

	exporterMode bool
}

func NewProposerHandler(duties *dutystore.Duties[eth2apiv1.ProposerDuty], exporterMode bool) *ProposerHandler {
	return &ProposerHandler{
		duties:           duties,
		exporterMode:     exporterMode,
		dutyFetchIntents: make(map[phase0.Epoch]bool),
	}
}

func (h *ProposerHandler) Name() string {
	return spectypes.BNRoleProposer.String()
}

func (h *ProposerHandler) WaitShutdown() {}

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
func (h *ProposerHandler) HandleDuties(ctx context.Context) {
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
			currentEpoch := h.beaconConfig.EstimatedEpochAtSlot(currentSlot)
			nextEpoch := currentEpoch + 1

			slotNumber := uint64(currentSlot)%h.beaconConfig.SlotsPerEpoch + 1
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
				tickCtx, cancel := context.WithDeadline(ctx, h.beaconConfig.SlotStartTime(currentSlot+1))
				defer cancel()

				// 1. Process the duty execution & fetching.

				// Process intents (if any): fetch & prepare the duties for the current epoch.
				h.prepareCurrentEpoch(tickCtx, logger, currentEpoch, currentSlot)

				h.processExecution(tickCtx, currentEpoch, currentSlot)

				// Process intents (if any): fetch & prepare the duties for the next epoch.
				h.prepareNextEpoch(tickCtx, logger, currentEpoch, currentSlot)

				// Clean up the irrelevant data to prevent infinite memory growth at the 1st slot of the epoch.
				if slotNumber == 1 && currentEpoch >= 1 {
					h.duties.EraseEpochData(currentEpoch - 1)
					delete(h.dutyFetchIntents, currentEpoch-1)
				}

				// 2. Process validator indices changes (if any). We want to process it on the current slot only
				// if we are still early into the slot (1 slot-interval is just a guesstimate), otherwise we might
				// be delaying the next tick (the duties that need to be executed on the next slot).

				indicesChangeDeadline := h.beaconConfig.SlotStartTime(currentSlot).Add(h.beaconConfig.IntervalDuration())
				select {
				case <-h.indicesChange:
					logger.Info("🔁 indices change received")

					// 1) Declare intents.
					// Some validator-related state has updated, means we need to re-fetch the duties for the current
					// and next epoch to ensure we have the up-to-date duties for all validators for both epochs.
					h.dutyFetchIntents[nextEpoch] = false
					h.dutyFetchIntents[currentEpoch] = false

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

				// 3. Schedule the duty-fetch for the next epoch, but only if it hasn't been scheduled already (also,
				// already fulfilled intents need not be re-scheduled).
				if _, ok := h.dutyFetchIntents[nextEpoch]; !ok {
					h.dutyFetchIntents[nextEpoch] = false
				}
			}()

		case reorgEvent := <-h.reorg:
			currentSlot := h.beaconConfig.EstimatedCurrentSlot()
			currentEpoch := h.beaconConfig.EstimatedEpochAtSlot(currentSlot)
			nextEpoch := currentEpoch + 1

			slotNumber := uint64(currentSlot)%h.beaconConfig.SlotsPerEpoch + 1
			buildStr := fmt.Sprintf("e%v-s%v-#%v", currentEpoch, currentSlot, slotNumber)
			refetchCurrentEpoch := reorgEvent.CurrentDutyDependentRootChanged ||
				(reorgEvent.PreviousDutyDependentRootChanged && reorgEvent.EpochTransition)
			logger := h.logger.With(
				zap.String("epoch_slot_pos", buildStr),
				zap.Uint64("current_epoch", uint64(currentEpoch)),
				zap.Uint64("current_slot", uint64(currentSlot)),
			)

			logger.Info("🔀 reorg event received",
				zap.Any("event", reorgEvent),
				zap.Bool("refetch_current_epoch_duties", refetchCurrentEpoch),
				zap.Bool("refetch_next_epoch_duties", true),
			)

			func() {
				// reorgCtx ensures we never take too long to process the reorg (we don't want to prevent the
				// slot-ticker from executing duties even if some of them might not be up to date). Since the
				// reorg can happen closer to the end of the current slot we wouldn't want to set the deadline
				// to currentSlot+1 as that might be too short (hence setting it to currentSlot+2).
				reorgCtx, cancel := context.WithDeadline(ctx, h.beaconConfig.SlotStartTime(currentSlot+2))
				defer cancel()

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
func (h *ProposerHandler) HandleInitialDuties(ctx context.Context) {
	// initCtx ensures we don't block indefinitely in case we can't fetch the duties on startup.
	initCtx, cancel := context.WithTimeout(ctx, h.beaconConfig.SlotDuration)
	defer cancel()

	currentSlot := h.beaconConfig.EstimatedCurrentSlot()
	currentEpoch := h.beaconConfig.EstimatedEpochAtSlot(currentSlot)
	nextEpoch := currentEpoch + 1

	slotNumber := uint64(currentSlot)%h.beaconConfig.SlotsPerEpoch + 1
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

func (h *ProposerHandler) prepareCurrentEpoch(ctx context.Context, logger *zap.Logger, currentEpoch phase0.Epoch, currentSlot phase0.Slot) {
	if fulfilled, ok := h.dutyFetchIntents[currentEpoch]; ok && !fulfilled {
		logger.Debug("fetching duties for the current epoch")

		err := h.fetchAndProcessDuties(ctx, logger, currentEpoch, currentSlot)
		if err != nil {
			logger.Error("fetching duties for the current epoch failed", zap.Error(err))
			return
		}
		h.dutyFetchIntents[currentEpoch] = true // the intent has been fulfilled

		logger.Debug("fetching duties for the current epoch succeeded")
	}
}

func (h *ProposerHandler) prepareNextEpoch(ctx context.Context, logger *zap.Logger, currentEpoch phase0.Epoch, currentSlot phase0.Slot) {
	// Delaying the duty fetch until it's a "good time" allows us to do it when the beacon node should be less busy.
	if fulfilled, ok := h.dutyFetchIntents[currentEpoch+1]; ok && !fulfilled && h.shouldFetchNextEpoch(currentSlot) {
		logger.Debug("fetching duties for the next epoch")

		err := h.fetchAndProcessDuties(ctx, logger, currentEpoch+1, currentSlot)
		if err != nil {
			logger.Error("fetching duties for the next epoch failed", zap.Error(err))
			return
		}
		h.dutyFetchIntents[currentEpoch+1] = true // the intent has been fulfilled

		logger.Debug("fetching duties for the next epoch succeeded")
	}
}

func (h *ProposerHandler) processExecution(ctx context.Context, epoch phase0.Epoch, slot phase0.Slot) {
	if h.exporterMode {
		return
	}

	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "proposer.execute"),
		trace.WithAttributes(
			observability.BeaconEpochAttribute(epoch),
			observability.BeaconSlotAttribute(slot),
			observability.BeaconRoleAttribute(spectypes.BNRoleProposer),
		))
	defer span.End()

	duties := h.duties.CommitteeSlotDuties(epoch, slot)
	if duties == nil {
		span.AddEvent("no duties available")
		span.SetStatus(codes.Ok, "")
		return
	}

	// range over duties and execute
	span.AddEvent("duties fetched", trace.WithAttributes(observability.DutyCountAttribute(len(duties))))
	toExecute := make([]*spectypes.ValidatorDuty, 0, len(duties))
	for _, d := range duties {
		if h.shouldExecute(d) {
			toExecute = append(toExecute, h.toSpecDuty(d, spectypes.BNRoleProposer))
		}
	}
	span.AddEvent("executing duties", trace.WithAttributes(observability.DutyCountAttribute(len(toExecute))))

	// Proposals need to be made within ~4s since the current slot start to be included on-chain, we'll make the
	// deadline to be 1 slot for simplicity.
	dutyDeadline := h.beaconConfig.SlotStartTime(slot + 1)
	h.dutiesExecutor.ExecuteDuties(ctx, toExecute, dutyDeadline)

	span.SetStatus(codes.Ok, "")
}

func (h *ProposerHandler) fetchAndProcessDuties(ctx context.Context, logger *zap.Logger, targetEpoch phase0.Epoch, currentSlot phase0.Slot) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "proposer.fetch_and_store"),
		trace.WithAttributes(
			observability.BeaconEpochAttribute(targetEpoch),
			observability.BeaconSlotAttribute(currentSlot),
			observability.BeaconRoleAttribute(spectypes.BNRoleProposer),
		))
	defer span.End()

	logger = logger.With(zap.Uint64("target_epoch", uint64(targetEpoch)))

	start := time.Now()

	var allEligibleIndices []phase0.ValidatorIndex
	for _, share := range h.validatorProvider.Validators() {
		if share.IsAttesting(targetEpoch) {
			allEligibleIndices = append(allEligibleIndices, share.ValidatorIndex)
		}
	}
	if len(allEligibleIndices) == 0 {
		const eventMsg = "no eligible validators for epoch"
		logger.Debug(eventMsg)
		span.AddEvent(eventMsg)
		span.SetStatus(codes.Ok, "")
		return nil
	}

	selfEligibleIndices := map[phase0.ValidatorIndex]struct{}{}
	for _, share := range h.validatorProvider.SelfValidators() {
		if share.IsAttesting(targetEpoch) {
			selfEligibleIndices[share.ValidatorIndex] = struct{}{}
		}
	}

	span.AddEvent("fetching duties from beacon node", trace.WithAttributes(observability.ValidatorCountAttribute(len(allEligibleIndices))))
	duties, err := h.beaconNode.ProposerDuties(ctx, targetEpoch, allEligibleIndices)
	if err != nil {
		return traces.Errorf(span, "failed to fetch proposer duties: %w", err)
	}

	specDuties := make([]*spectypes.ValidatorDuty, 0, len(duties))
	storeDuties := make([]dutystore.StoreDuty[eth2apiv1.ProposerDuty], 0, len(duties))
	for _, d := range duties {
		_, inCommitteeDuty := selfEligibleIndices[d.ValidatorIndex]
		storeDuties = append(storeDuties, dutystore.StoreDuty[eth2apiv1.ProposerDuty]{
			Slot:           d.Slot,
			ValidatorIndex: d.ValidatorIndex,
			Duty:           d,
			InCommittee:    inCommitteeDuty,
		})
		span.AddEvent("will store duty", trace.WithAttributes(observability.ValidatorIndexAttribute(d.ValidatorIndex)))
		specDuties = append(specDuties, h.toSpecDuty(d, spectypes.BNRoleProposer))
	}

	span.AddEvent("storing duties", trace.WithAttributes(observability.DutyCountAttribute(len(storeDuties))))
	h.duties.Set(targetEpoch, storeDuties)

	truncate := -1
	if h.exporterMode {
		truncate = 10
	}
	logger.Debug("📚 got duties",
		fields.Count(len(duties)),
		fields.Duties(targetEpoch, specDuties, truncate),
		fields.Took(time.Since(start)),
	)

	span.SetStatus(codes.Ok, "")
	return nil
}

func (h *ProposerHandler) toSpecDuty(duty *eth2apiv1.ProposerDuty, role spectypes.BeaconRole) *spectypes.ValidatorDuty {
	return &spectypes.ValidatorDuty{
		Type:           role,
		PubKey:         duty.PubKey,
		Slot:           duty.Slot,
		ValidatorIndex: duty.ValidatorIndex,
	}
}

func (h *ProposerHandler) shouldExecute(duty *eth2apiv1.ProposerDuty) bool {
	currentSlot := h.beaconConfig.EstimatedCurrentSlot()
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
