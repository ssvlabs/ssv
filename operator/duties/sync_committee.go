package duties

import (
	"context"
	"fmt"
	"strings"
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

type SyncCommitteeHandler struct {
	baseHandler

	duties *dutystore.SyncCommitteeDuties

	// dutyFetchIntents stores the intents to fetch duties for some target periods, the bool indicates whether the
	// intent has already been fulfilled.
	dutyFetchIntents map[uint64]bool

	// preparationSlots is the number of slots ahead of the sync committee
	// period change at which to prepare the relevant duties.
	preparationSlots uint64

	// backgroundTasks tracks all go-routines spawned by Scheduler for graceful shutdown.
	backgroundTasks sync.WaitGroup

	exporterMode bool
}

func NewSyncCommitteeHandler(duties *dutystore.SyncCommitteeDuties, exporterMode bool) *SyncCommitteeHandler {
	h := &SyncCommitteeHandler{
		duties:           duties,
		dutyFetchIntents: make(map[uint64]bool),
		exporterMode:     exporterMode,
	}
	return h
}

func (h *SyncCommitteeHandler) Name() string {
	return spectypes.BNRoleSyncCommittee.String()
}

func (h *SyncCommitteeHandler) WaitShutdown() {
	h.backgroundTasks.Wait()
}

// HandleDuties manages the duty lifecycle, handling different cases:
//
// On First Run:
//  1. If necessary, fetch duties for the current period.
//  2. If necessary, fetch duties for the next period.
//  3. Duties will be executed on the very next slot-tick.
//
// On Re-org:
//  1. If the current duty dependent root changed, declare the intent to fetch duties for the next period
//     (the current period's sync committee membership is fixed a full period in advance, so it is unaffected).
//  2. If necessary, pre-fetch the next period's duties so they can be processed on the next slot-tick.
//  3. Duties will be executed on the very next slot-tick.
//
// On Ticker event:
//  1. If necessary, fetch duties for the current period.
//  2. Execute duties.
//  3. If necessary, fetch duties for the next period.
//  4. If necessary, process validator-indices changes by declaring the intents to fetch duties for the periods
//     affected by it, also potentially pre-fetching duties so they are ready for processing on the next slot-tick.
func (h *SyncCommitteeHandler) HandleDuties(ctx context.Context) {
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
			currentPeriod := h.netCfg.EstimatedSyncCommitteePeriodAtEpoch(currentEpoch)
			nextPeriod := currentPeriod + 1

			slotNumber := uint64(currentSlot)%h.netCfg.SlotsPerEpoch + 1
			buildStr := fmt.Sprintf("p%v-e%v-s%v-#%v", currentPeriod, currentEpoch, currentSlot, slotNumber)
			logger := h.logger.With(
				zap.String("period_epoch_slot_pos", buildStr),
				zap.Uint64("current_period", currentPeriod),
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

				// Process intents (if any): fetch & prepare the duties for the current period.
				h.prepareCurrentPeriod(tickCtx, logger, currentPeriod, currentEpoch, currentSlot, true)

				if !h.netCfg.BooleForkAtSlot(currentSlot) {
					// Before Boole fork: execute Alan sync committee contribution flow.
					h.processExecution(tickCtx, currentPeriod, currentSlot)
				}

				// After Boole fork: keep fetching duties (to pass them to both Committee and AggregatorCommittee handlers),
				// but skip Alan execution, as the aggregator committee handler will be responsible for executing them.
				// Process intents (if any): fetch & prepare the duties for the next period.
				h.prepareNextPeriod(tickCtx, logger, currentPeriod, currentEpoch, currentSlot, true)

				// Clean up the irrelevant data to prevent infinite memory growth at the very 1st slot of the epoch.
				// Note, it doesn't have to be "the very 1st slot" exactly - it's just the most natural time to do it.
				if slotNumber == 1 && currentPeriod >= 1 {
					h.duties.Reset(currentPeriod - 1)
					delete(h.dutyFetchIntents, currentPeriod-1)
				}

				// 2. Schedule the next-period duty-fetch intent (only if absent, so a pending or fulfilled one is
				// left untouched). We register it before the indices-change handling below, which can return early
				// on tickCtx.Done - so even a tick that overruns its deadline still records the intent, and the
				// next-period pre-fetch can't be deferred indefinitely at a period boundary under sustained slowness.
				if _, ok := h.dutyFetchIntents[nextPeriod]; !ok {
					h.dutyFetchIntents[nextPeriod] = false
				}

				// 3. Process validator indices changes (if any). We want to process it on the current slot only
				// if we are still early into the slot (1 slot-interval is just a guesstimate), otherwise we might
				// be delaying the next tick (the duties that need to be executed on the next slot).

				indicesChangeDeadline := h.netCfg.SlotStartTime(currentSlot).Add(h.netCfg.IntervalDuration())
				select {
				case <-h.indicesChangeCh:
					logger.Info("🔁 indices change received")

					// 1) Declare intents.
					// Some validator-related state has changed, so re-fetch the duties for the current and next
					// period to keep them up to date for all validators.
					h.dutyFetchIntents[currentPeriod] = false
					h.dutyFetchIntents[nextPeriod] = false

					// 2) Process certain intents immediately.
					// When at period boundary, we only care about pre-fetching & preparing the duties for the next
					// period (the current period will have been passed upon the next slot-tick). Otherwise, pre-fetch &
					// prepare the duties for the current period.
					if h.atLastSlotOrPastCurrentPeriod(currentSlot, currentPeriod) {
						delete(h.dutyFetchIntents, currentPeriod) // optimization: prune irrelevant intent
						h.prepareNextPeriod(tickCtx, logger, currentPeriod, currentEpoch, currentSlot, true)
					} else {
						h.prepareCurrentPeriod(tickCtx, logger, currentPeriod, currentEpoch, currentSlot, true)
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
			currentPeriod := h.netCfg.EstimatedSyncCommitteePeriodAtEpoch(currentEpoch)
			nextPeriod := currentPeriod + 1

			slotNumber := uint64(currentSlot)%h.netCfg.SlotsPerEpoch + 1
			buildStr := fmt.Sprintf("p%v-e%v-s%v-#%v", currentPeriod, currentEpoch, currentSlot, slotNumber)
			logger := h.logger.With(
				zap.String("period_epoch_slot_pos", buildStr),
				zap.Uint64("current_period", currentPeriod),
				zap.Uint64("current_epoch", uint64(currentEpoch)),
				zap.Uint64("current_slot", uint64(currentSlot)),
			)

			// Sync committee membership for the current period is fixed a full period in advance, so a reorg can
			// only affect the next period's duties (and only when the current duty dependent root changed).
			refetchNextPeriod := reorgEvent.CurrentDutyDependentRootChanged

			logger.Info("🔀 reorg event received",
				zap.Any("event", reorgEvent),
				zap.Bool("refetch_next_period_duties", refetchNextPeriod),
			)

			func() {
				// reorgCtx ensures we never take too long to process the reorg (we don't want to prevent the
				// slot-ticker from executing duties even if some of them might not be up to date). Since the
				// reorg can happen closer to the end of the current slot we wouldn't want to set the deadline
				// to currentSlot+1 as that might be too short (hence setting it to currentSlot+2).
				reorgCtx, cancel := context.WithDeadline(ctx, h.netCfg.SlotStartTime(currentSlot+2))
				defer cancel()

				if !refetchNextPeriod {
					return
				}

				// 1) Declare intent.
				// We deliberately do NOT Reset the existing next-period duties before re-fetching: if the re-fetch
				// fails we keep serving the previously fetched (possibly stale) duties and retry on later ticks,
				// rather than dropping a whole period's duties on a transient error. A successful re-fetch overwrites them.
				h.dutyFetchIntents[nextPeriod] = false

				// 2) Process the intent immediately (when it's a "good time") so the duties are ready for the
				// next slot-tick.
				h.prepareNextPeriod(reorgCtx, logger, currentPeriod, currentEpoch, currentSlot, true)
			}()
		}
	}
}

// HandleInitialDuties fetches & prepares the duties for the current and next periods.
func (h *SyncCommitteeHandler) HandleInitialDuties(ctx context.Context) {
	initCtx, cancel := context.WithTimeout(ctx, h.netCfg.SlotDuration)
	defer cancel()

	// Prepare the next-period duties 1.5 epochs ahead of the period change, when the beacon node is likely less busy.
	h.preparationSlots = h.netCfg.SlotsPerEpoch * 3 / 2

	currentSlot := h.netCfg.EstimatedCurrentSlot()
	currentEpoch := h.netCfg.EstimatedEpochAtSlot(currentSlot)
	currentPeriod := h.netCfg.EstimatedSyncCommitteePeriodAtEpoch(currentEpoch)
	nextPeriod := currentPeriod + 1

	slotNumber := uint64(currentSlot)%h.netCfg.SlotsPerEpoch + 1
	buildStr := fmt.Sprintf("p%v-e%v-s%v-#%v", currentPeriod, currentEpoch, currentSlot, slotNumber)
	logger := h.logger.With(
		zap.String("period_epoch_slot_pos", buildStr),
		zap.Uint64("current_period", currentPeriod),
		zap.Uint64("current_epoch", uint64(currentEpoch)),
		zap.Uint64("current_slot", uint64(currentSlot)),
	)

	// 1) Declare intents.
	h.dutyFetchIntents[currentPeriod] = false
	h.dutyFetchIntents[nextPeriod] = false

	// 2) Process certain intents immediately.
	// At the last slot of current period we don't fetch duties for the current period because we likely won't
	// have enough time to process those duties anyway ... but we do want to fetch the duties for the next period
	// right away in that case since we'll need to be able to execute those duties on the next tick - the tick
	// corresponding to the 1st slot of the next period.
	if h.atLastSlotOrPastCurrentPeriod(currentSlot, currentPeriod) {
		delete(h.dutyFetchIntents, currentPeriod) // optimization: prune irrelevant intent
		h.prepareNextPeriod(initCtx, logger, currentPeriod, currentEpoch, currentSlot, false)
	} else {
		h.prepareCurrentPeriod(initCtx, logger, currentPeriod, currentEpoch, currentSlot, false)
	}
}

func (h *SyncCommitteeHandler) prepareCurrentPeriod(
	ctx context.Context,
	logger *zap.Logger,
	currentPeriod uint64,
	currentEpoch phase0.Epoch,
	currentSlot phase0.Slot,
	waitForInit bool,
) {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "sync_committee.prepare_current_period"),
		trace.WithAttributes(
			observability.BeaconPeriodAttribute(currentPeriod),
			observability.BeaconSlotAttribute(currentSlot),
			observability.BeaconRoleAttribute(spectypes.BNRoleSyncCommittee),
		))
	defer span.End()

	if fulfilled, ok := h.dutyFetchIntents[currentPeriod]; ok && !fulfilled {
		logger.Debug("fetching duties for the current period")

		err := h.fetchAndProcessDuties(ctx, logger, currentPeriod, currentEpoch, currentSlot, waitForInit)
		if err != nil {
			logger.Error("fetching duties for the current period failed", zap.Error(err))
			span.SetStatus(codes.Error, err.Error())
			return
		}
		h.dutyFetchIntents[currentPeriod] = true

		logger.Debug("fetching duties for the current period succeeded")
	}

	span.SetStatus(codes.Ok, "")
}

func (h *SyncCommitteeHandler) prepareNextPeriod(
	ctx context.Context,
	logger *zap.Logger,
	currentPeriod uint64,
	currentEpoch phase0.Epoch,
	currentSlot phase0.Slot,
	waitForInit bool,
) {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "sync_committee.prepare_next_period"),
		trace.WithAttributes(
			observability.BeaconPeriodAttribute(currentPeriod+1),
			observability.BeaconSlotAttribute(currentSlot),
			observability.BeaconRoleAttribute(spectypes.BNRoleSyncCommittee),
		))
	defer span.End()

	// Delaying the duty fetch until it's a "good time" allows us to do it when the beacon node should be less busy.
	if fulfilled, ok := h.dutyFetchIntents[currentPeriod+1]; ok && !fulfilled && h.shouldFetchNextPeriod(currentSlot) {
		logger.Debug("fetching duties for the next period")

		err := h.fetchAndProcessDuties(ctx, logger, currentPeriod+1, currentEpoch, currentSlot, waitForInit)
		if err != nil {
			logger.Error("fetching duties for the next period failed", zap.Error(err))
			span.SetStatus(codes.Error, err.Error())
			return
		}
		h.dutyFetchIntents[currentPeriod+1] = true

		logger.Debug("fetching duties for the next period succeeded")
	}

	span.SetStatus(codes.Ok, "")
}

func (h *SyncCommitteeHandler) processExecution(ctx context.Context, period uint64, slot phase0.Slot) {
	if h.exporterMode {
		return
	}

	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "sync_committee_contribution.execute"),
		trace.WithAttributes(
			observability.BeaconSlotAttribute(slot),
			observability.BeaconPeriodAttribute(period),
			observability.BeaconRoleAttribute(spectypes.BNRoleSyncCommitteeContribution),
		))
	defer span.End()

	// range over duties and execute
	duties := h.duties.CommitteePeriodDuties(period)
	if duties == nil {
		span.AddEvent("no duties available")
		span.SetStatus(codes.Ok, "")
		return
	}

	span.AddEvent("duties fetched", trace.WithAttributes(observability.DutyCountAttribute(len(duties))))
	toExecute := make([]*spectypes.ValidatorDuty, 0, len(duties))
	for _, d := range duties {
		if h.shouldExecute(d, slot) {
			toExecute = append(toExecute, h.toSpecDuty(d, slot, spectypes.BNRoleSyncCommitteeContribution))
		}
	}
	span.AddEvent("executing duties", trace.WithAttributes(observability.DutyCountAttribute(len(toExecute))))

	// Sync committee contributions are rewarded as long as they are included within 1 slot of their target slot
	// (i.e., from target slot up to and including target + 1).
	dutyDeadline := h.netCfg.SlotStartTime(slot + 1)
	h.dutiesExecutor.ExecuteDuties(ctx, toExecute, dutyDeadline)

	span.SetStatus(codes.Ok, "")
}

// fetchAndProcessDuties fetches & stores the sync committee duties for the given period (current or future).
// The passed epoch must be the current epoch; for a future period the target epoch is resolved to that
// period's first epoch.
func (h *SyncCommitteeHandler) fetchAndProcessDuties(
	ctx context.Context,
	logger *zap.Logger,
	period uint64,
	epoch phase0.Epoch,
	currentSlot phase0.Slot,
	waitForInit bool,
) error {
	start := time.Now()
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "sync_committee.fetch_and_store"),
		trace.WithAttributes(
			observability.BeaconPeriodAttribute(period),
			observability.BeaconEpochAttribute(epoch),
			observability.BeaconSlotAttribute(currentSlot),
			observability.BeaconRoleAttribute(spectypes.BNRoleSyncCommittee),
		))
	defer span.End()

	if period > h.netCfg.EstimatedSyncCommitteePeriodAtEpoch(epoch) {
		epoch = h.netCfg.FirstEpochOfSyncPeriod(period)
	}

	span.SetAttributes(observability.BeaconEpochAttribute(epoch))
	logger = logger.With(
		zap.Uint64("target_period", period),
		zap.Uint64("target_epoch", uint64(epoch)),
	)

	eligibleIndices := h.validatorController.FilterIndices(waitForInit, func(s *types.SSVShare) bool {
		return s.IsParticipating(h.netCfg.Beacon, epoch)
	})

	if len(eligibleIndices) == 0 {
		const eventMsg = "no eligible validators for period"
		logger.Debug(eventMsg)
		span.AddEvent(eventMsg)
		span.SetStatus(codes.Ok, "")
		return nil
	}

	span.AddEvent("fetching duties from beacon node", trace.WithAttributes(observability.ValidatorCountAttribute(len(eligibleIndices))))
	duties, err := h.beaconNode.SyncCommitteeDuties(ctx, epoch, eligibleIndices)
	if err != nil {
		return traces.Errorf(span, "failed to fetch sync committee duties: %w", err)
	}

	selfShares := h.validatorProvider.SelfParticipatingValidators(epoch)
	selfIndices := make(map[phase0.ValidatorIndex]struct{}, len(selfShares))
	for _, share := range selfShares {
		selfIndices[share.ValidatorIndex] = struct{}{}
	}

	storeDuties := make([]dutystore.StoreSyncCommitteeDuty, 0, len(duties))
	for _, duty := range duties {
		_, inCommittee := selfIndices[duty.ValidatorIndex]
		storeDuties = append(storeDuties, dutystore.StoreSyncCommitteeDuty{
			ValidatorIndex: duty.ValidatorIndex,
			Duty:           duty,
			InCommittee:    inCommittee,
		})
		span.AddEvent("will store duty", trace.WithAttributes(observability.ValidatorIndexAttribute(duty.ValidatorIndex)))
	}

	span.AddEvent("storing duties", trace.WithAttributes(observability.DutyCountAttribute(len(storeDuties))))
	h.duties.Set(period, storeDuties)

	h.logDutiesFetched(logger, period, duties, start)

	// Further processing is not needed in exporter mode, terminate early
	// avoiding CL subscriptions saves some CPU & Network resources
	// and avoids unnecessary log noise
	if h.exporterMode {
		span.SetStatus(codes.Ok, "")
		return nil
	}

	// lastEpoch + 1 because the subscription's "until" epoch is exclusive
	lastEpoch := h.netCfg.FirstEpochOfSyncPeriod(period+1) - 1
	subscriptions := calculateSubscriptions(lastEpoch+1, duties)

	if len(subscriptions) == 0 {
		span.AddEvent("no subscriptions available")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	span.AddEvent("submitting beacon sync committee subscriptions", trace.WithAttributes(
		attribute.Int("ssv.validator.duty.subscriptions", len(subscriptions)),
	))

	h.backgroundTasks.Add(1)
	go func() {
		defer h.backgroundTasks.Done()

		// Cannot use parent-context itself here, have to create independent instance
		// to be able to continue working in background.
		subscriptionCtx, cancel := context.WithCancel(h.ctx)
		defer cancel()

		if err := h.beaconNode.SubmitSyncCommitteeSubscriptions(subscriptionCtx, subscriptions); err != nil {
			h.logger.Error("failed to subscribe sync committee to subnet", zap.Error(err))
		}
	}()

	span.SetStatus(codes.Ok, "")
	return nil
}

func (h *SyncCommitteeHandler) logDutiesFetched(
	logger *zap.Logger,
	period uint64,
	duties []*eth2apiv1.SyncCommitteeDuty,
	start time.Time,
) {
	var b strings.Builder
	if h.exporterMode {
		// too many duties to log individually
		b.WriteString("[exporter mode]")
	} else {
		for i, duty := range duties {
			if i > 0 {
				b.WriteString(", ")
			}
			tmp := fmt.Sprintf("%v-p%v-v%v", h.Name(), period, duty.ValidatorIndex)
			b.WriteString(tmp)
		}
	}
	logger.Debug("👥 got duties",
		fields.Count(len(duties)),
		zap.String("period", fmt.Sprintf("p%v", period)),
		zap.Any("duties", b.String()),
		fields.Took(time.Since(start)),
	)
}

func (h *SyncCommitteeHandler) toSpecDuty(duty *eth2apiv1.SyncCommitteeDuty, slot phase0.Slot, role spectypes.BeaconRole) *spectypes.ValidatorDuty {
	indices := make([]uint64, len(duty.ValidatorSyncCommitteeIndices))
	for i, index := range duty.ValidatorSyncCommitteeIndices {
		indices[i] = uint64(index)
	}
	return &spectypes.ValidatorDuty{
		Type:                          role,
		PubKey:                        duty.PubKey,
		Slot:                          slot, // in order for the duty scheduler to execute
		ValidatorIndex:                duty.ValidatorIndex,
		ValidatorSyncCommitteeIndices: indices,
	}
}

func (h *SyncCommitteeHandler) shouldExecute(duty *eth2apiv1.SyncCommitteeDuty, slot phase0.Slot) bool {
	currentSlot := h.netCfg.EstimatedCurrentSlot()

	_, exists := h.validatorProvider.Validator(duty.PubKey[:])
	if !exists {
		h.logger.Warn("validator not found", fields.Validator(duty.PubKey[:]))
		return false
	}

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

// calculateSubscriptions calculates the sync committee subscriptions given a set of duties.
func calculateSubscriptions(endEpoch phase0.Epoch, duties []*eth2apiv1.SyncCommitteeDuty) []*eth2apiv1.SyncCommitteeSubscription {
	subscriptions := make([]*eth2apiv1.SyncCommitteeSubscription, 0, len(duties))
	for _, duty := range duties {
		subscriptions = append(subscriptions, &eth2apiv1.SyncCommitteeSubscription{
			ValidatorIndex:       duty.ValidatorIndex,
			SyncCommitteeIndices: duty.ValidatorSyncCommitteeIndices,
			UntilEpoch:           endEpoch,
		})
	}

	return subscriptions
}

func (h *SyncCommitteeHandler) shouldFetchNextPeriod(slot phase0.Slot) bool {
	periodSlots := h.slotsPerPeriod()
	return uint64(slot)%periodSlots > periodSlots-h.preparationSlots-2
}

func (h *SyncCommitteeHandler) slotsPerPeriod() uint64 {
	return h.netCfg.EpochsPerSyncCommitteePeriod * h.netCfg.SlotsPerEpoch
}
