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
	"github.com/ssvlabs/ssv/protocol/v2/types"
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
		dutyFetchIntents: make(map[phase0.Epoch]bool),
		exporterMode:     exporterMode,
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

				h.processExecution(tickCtx, currentEpoch, currentSlot)

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

			// Proposer duties for the current epoch are determined by the "current duty dependent root",
			// so we re-fetch the current epoch only if it has changed. The next epoch is always re-fetched
			// on any reorg to ensure we have the up-to-date duties for all validators.
			refetchCurrentEpoch := reorgEvent.CurrentDutyDependentRootChanged

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
func (h *ProposerHandler) HandleInitialDuties(ctx context.Context) {
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

func (h *ProposerHandler) prepareCurrentEpoch(ctx context.Context, logger *zap.Logger, currentEpoch phase0.Epoch, currentSlot phase0.Slot) {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "proposer.prepare_current_epoch"),
		trace.WithAttributes(
			observability.BeaconEpochAttribute(currentEpoch),
			observability.BeaconSlotAttribute(currentSlot),
			observability.BeaconRoleAttribute(spectypes.BNRoleProposer),
		))
	defer span.End()

	if fulfilled, ok := h.dutyFetchIntents[currentEpoch]; ok && !fulfilled {
		fetched, err := h.fetchAndProcessDuties(ctx, logger, currentEpoch, currentSlot)
		if err != nil {
			logger.Error("fetching duties for the current epoch failed", zap.Error(err))
			span.SetStatus(codes.Error, err.Error())
			return
		}
		// Fulfil the intent only if a fetch actually ran; a not-yet-eligible epoch stays pending so a later tick retries.
		if fetched {
			h.dutyFetchIntents[currentEpoch] = true
		}
	}

	span.SetStatus(codes.Ok, "")
}

func (h *ProposerHandler) prepareNextEpoch(ctx context.Context, logger *zap.Logger, currentEpoch phase0.Epoch, currentSlot phase0.Slot) {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "proposer.prepare_next_epoch"),
		trace.WithAttributes(
			observability.BeaconEpochAttribute(currentEpoch+1),
			observability.BeaconSlotAttribute(currentSlot),
			observability.BeaconRoleAttribute(spectypes.BNRoleProposer),
		))
	defer span.End()

	// Delaying the duty fetch until it's a "good time" allows us to do it when the beacon node should be less busy.
	if fulfilled, ok := h.dutyFetchIntents[currentEpoch+1]; ok && !fulfilled && h.shouldFetchNextEpoch(currentSlot) {
		fetched, err := h.fetchAndProcessDuties(ctx, logger, currentEpoch+1, currentSlot)
		if err != nil {
			logger.Error("fetching duties for the next epoch failed", zap.Error(err))
			span.SetStatus(codes.Error, err.Error())
			return
		}
		// Fulfil the intent only if a fetch actually ran; a not-yet-eligible epoch stays pending so a later tick retries.
		if fetched {
			h.dutyFetchIntents[currentEpoch+1] = true
		}
	}

	span.SetStatus(codes.Ok, "")
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
	dutyDeadline := h.netCfg.SlotStartTime(slot + 1)
	h.dutiesExecutor.ExecuteDuties(ctx, toExecute, dutyDeadline)

	span.SetStatus(codes.Ok, "")
}

// fetchAndProcessDuties fetches and stores the epoch's proposer duties. It returns fetched=false (with a
// nil error) when no validators are eligible yet — a not-ready state (e.g. beacon metadata not synced) the
// caller must retry rather than treat as fulfilled; fetched=true means a beacon fetch actually ran.
func (h *ProposerHandler) fetchAndProcessDuties(ctx context.Context, logger *zap.Logger, targetEpoch phase0.Epoch, currentSlot phase0.Slot) (fetched bool, err error) {
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
		h.logNoEligibleDiagnostic(logger, targetEpoch) // TEMP(ssvlabs/ssv#2901): remove after devnet confirmation
		span.AddEvent(eventMsg)
		span.SetStatus(codes.Ok, "")
		// No eligible validators yet — not a fulfilled fetch; caller retries on a later tick.
		return false, nil
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
		return false, traces.Errorf(span, "failed to fetch proposer duties: %w", err)
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
		fields.Duties(targetEpoch, specDuties, truncate, func(duty *spectypes.ValidatorDuty) spectypes.RunnerRole {
			return types.RunnerRoleForValidatorDuty(duty, h.netCfg.BooleForkAtSlot(duty.Slot))
		}),
		fields.Took(time.Since(start)),
	)

	span.SetStatus(codes.Ok, "")
	return true, nil
}

// logNoEligibleDiagnostic is a TEMPORARY diagnostic (ssvlabs/ssv#2901) for the "every proposer slot missed
// on the Gloas devnet" investigation. On zero eligible validators it dumps Validators() vs SelfValidators()
// so a devnet run can distinguish metadata-not-synced-yet (shares present but IsAttesting=false; the retry
// fix recovers these) from a diverging/empty Validators() view (self_attesting>0 yet none eligible; a
// different root cause the retry would not fix). Remove once the root cause is confirmed on devnet.
func (h *ProposerHandler) logNoEligibleDiagnostic(logger *zap.Logger, targetEpoch phase0.Epoch) {
	all := h.validatorProvider.Validators()
	self := h.validatorProvider.SelfValidators()

	selfAttesting := 0
	for _, s := range self {
		if s.IsAttesting(targetEpoch) {
			selfAttesting++
		}
	}

	const sampleCap = 16
	samples := make([]string, 0, min(len(all), sampleCap))
	for i, s := range all {
		if i >= sampleCap {
			break
		}
		samples = append(samples, fmt.Sprintf("idx=%d status=%s hasMeta=%t attesting=%t liquidated=%t",
			s.ValidatorIndex, s.Status, s.HasBeaconMetadata(), s.IsAttesting(targetEpoch), s.Liquidated))
	}

	logger.Debug("🔬 no eligible validators for epoch (diagnostic)",
		zap.Uint64("target_epoch", uint64(targetEpoch)),
		zap.Int("validators_total", len(all)),
		zap.Int("self_validators", len(self)),
		zap.Int("self_attesting", selfAttesting),
		zap.Strings("shares", samples),
	)
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
	currentSlot := h.netCfg.EstimatedCurrentSlot()
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
