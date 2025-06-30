package duties

import (
	"context"
	"fmt"
	"strings"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

type SyncCommitteeHandler struct {
	baseHandler

	duties             *dutystore.SyncCommitteeDuties
	fetchCurrentPeriod bool
	fetchNextPeriod    bool

	// preparationSlots is the number of slots ahead of the sync committee
	// period change at which to prepare the relevant duties.
	preparationSlots uint64
}

func NewSyncCommitteeHandler(duties *dutystore.SyncCommitteeDuties) *SyncCommitteeHandler {
	h := &SyncCommitteeHandler{
		duties: duties,
	}
	h.fetchCurrentPeriod = true
	return h
}

func (h *SyncCommitteeHandler) Name() string {
	return spectypes.BNRoleSyncCommittee.String()
}

// HandleDuties manages the duty lifecycle, handling different cases:
//
// On First Run:
//  1. Fetch duties for the current period.
//  2. If necessary, fetch duties for the next period.
//  3. Execute duties.
//
// On Re-org:
//  1. Execute duties.
//  2. If necessary, fetch duties for the next period.
//
// On Indices Change:
//  1. Execute duties.
//  2. ResetEpoch duties for the current period.
//  3. Fetch duties for the current period.
//  4. If necessary, fetch duties for the next period.
//
// On Ticker event:
//  1. Execute duties.
//  2. If necessary, fetch duties for the next period.
func (h *SyncCommitteeHandler) HandleDuties(ctx context.Context) {
	h.logger.Info("starting duty handler")
	defer h.logger.Info("duty handler exited")

	// Prepare relevant duties 1.5 epochs (48 slots) ahead of the sync committee period change.
	// The 1.5 epochs timing helps ensure setup occurs when the beacon node is likely less busy.
	h.preparationSlots = h.beaconConfig.GetSlotsPerEpoch() * 3 / 2

	if h.shouldFetchNextPeriod(h.beaconConfig.EstimatedCurrentSlot()) {
		h.fetchNextPeriod = true
	}

	next := h.ticker.Next()
	for {
		select {
		case <-ctx.Done():
			return

		case <-next:
			slot := h.ticker.Slot()
			next = h.ticker.Next()
			epoch := h.beaconConfig.EstimatedEpochAtSlot(slot)
			period := h.beaconConfig.EstimatedSyncCommitteePeriodAtEpoch(epoch)
			buildStr := fmt.Sprintf("p%v-e%v-s%v-#%v", period, epoch, slot, slot%32+1)
			h.logger.Debug("🛠 ticker event", zap.String("period_epoch_slot_pos", buildStr))

			ctx, cancel := context.WithDeadline(ctx, h.beaconConfig.GetSlotStartTime(slot+1).Add(100*time.Millisecond))
			h.processExecution(ctx, period, slot)
			h.processFetching(ctx, epoch, period, true)
			cancel()

			// if we have reached the preparation slots -1, prepare the next period duties in the next slot.
			periodSlots := h.slotsPerPeriod()
			if uint64(slot)%periodSlots == periodSlots-h.preparationSlots-1 {
				h.fetchNextPeriod = true
			}

			// last slot of period
			if slot == h.beaconConfig.LastSlotOfSyncPeriod(period) {
				h.duties.Reset(period - 1)
			}

		case reorgEvent := <-h.reorg:
			epoch := h.beaconConfig.EstimatedEpochAtSlot(reorgEvent.Slot)
			period := h.beaconConfig.EstimatedSyncCommitteePeriodAtEpoch(epoch)

			buildStr := fmt.Sprintf("p%v-e%v-s%v-#%v", period, epoch, reorgEvent.Slot, reorgEvent.Slot%32+1)
			h.logger.Info("🔀 reorg event received", zap.String("period_epoch_slot_pos", buildStr), zap.Any("event", reorgEvent))

			// reset current epoch duties
			if reorgEvent.Current && h.shouldFetchNextPeriod(reorgEvent.Slot) {
				h.duties.Reset(period + 1)
				h.fetchNextPeriod = true
			}

		case <-h.indicesChange:
			slot := h.beaconConfig.EstimatedCurrentSlot()
			epoch := h.beaconConfig.EstimatedEpochAtSlot(slot)
			period := h.beaconConfig.EstimatedSyncCommitteePeriodAtEpoch(epoch)
			buildStr := fmt.Sprintf("p%v-e%v-s%v-#%v", period, epoch, slot, slot%32+1)
			h.logger.Info("🔁 indices change received", zap.String("period_epoch_slot_pos", buildStr))

			h.fetchCurrentPeriod = true

			// reset next period duties if in appropriate slot range
			if h.shouldFetchNextPeriod(slot) {
				h.fetchNextPeriod = true
			}
		}
	}
}

func (h *SyncCommitteeHandler) HandleInitialDuties(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, h.beaconConfig.GetSlotDuration()/2)
	defer cancel()

	epoch := h.beaconConfig.EstimatedCurrentEpoch()
	period := h.beaconConfig.EstimatedSyncCommitteePeriodAtEpoch(epoch)
	h.processFetching(ctx, epoch, period, false)
}

func (h *SyncCommitteeHandler) processFetching(ctx context.Context, epoch phase0.Epoch, period uint64, waitForInitial bool) {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "sync_committee.fetch"),
		trace.WithAttributes(
			observability.BeaconEpochAttribute(epoch),
			observability.BeaconPeriodAttribute(period),
		))
	defer span.End()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if h.fetchCurrentPeriod {
		span.AddEvent("fetching current period duties")
		if err := h.fetchAndProcessDuties(ctx, epoch, period, waitForInitial); err != nil {
			h.logger.Error("failed to fetch duties for current epoch", zap.Error(err))
			span.SetStatus(codes.Error, err.Error())
			return
		}
		h.fetchCurrentPeriod = false
	}

	if h.fetchNextPeriod {
		span.AddEvent("fetching next period duties")
		if err := h.fetchAndProcessDuties(ctx, epoch, period+1, waitForInitial); err != nil {
			h.logger.Error("failed to fetch duties for next epoch", zap.Error(err))
			span.SetStatus(codes.Error, err.Error())
			return
		}
		h.fetchNextPeriod = false
	}

	span.SetStatus(codes.Ok, "")
}

func (h *SyncCommitteeHandler) processExecution(ctx context.Context, period uint64, slot phase0.Slot) {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "sync_committee.execute"),
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

	h.dutiesExecutor.ExecuteDuties(ctx, toExecute)

	span.SetStatus(codes.Ok, "")
}

// Period can be current or future.
// If the period passed is the current period – the sync committee target epoch should be the current epoch.
// If the period passed is a future period – the sync committee target epoch should be the first epoch of that future period.
// The epoch passed is always the current epoch.
func (h *SyncCommitteeHandler) fetchAndProcessDuties(ctx context.Context, epoch phase0.Epoch, period uint64, waitForInitial bool) error {
	start := time.Now()
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "sync_committee.fetch_and_store"),
		trace.WithAttributes(observability.BeaconPeriodAttribute(period)))
	defer span.End()

	if period > h.beaconConfig.EstimatedSyncCommitteePeriodAtEpoch(epoch) {
		epoch = h.beaconConfig.FirstEpochOfSyncPeriod(period)
	}

	span.SetAttributes(observability.BeaconEpochAttribute(epoch))

	eligibleIndices := h.validatorController.FilterIndices(waitForInitial, func(s *types.SSVShare) bool {
		return s.IsParticipating(h.beaconConfig, epoch)
	})

	if len(eligibleIndices) == 0 {
		const eventMsg = "no eligible validators for period"
		h.logger.Debug(eventMsg, fields.Epoch(epoch), zap.Uint64("period", period))
		span.AddEvent(eventMsg)
		span.SetStatus(codes.Ok, "")
		return nil
	}

	span.AddEvent("fetching duties from beacon node", trace.WithAttributes(observability.ValidatorCountAttribute(len(eligibleIndices))))
	duties, err := h.beaconNode.SyncCommitteeDuties(ctx, epoch, eligibleIndices)
	if err != nil {
		return observability.Errorf(span, "failed to fetch sync committee duties: %w", err)
	}

	selfSnapshots := h.validatorProvider.GetSelfParticipatingValidators(epoch, registrystorage.ParticipationOptions{
		IncludeLiquidated: false,
		IncludeExited:     false,
		OnlyAttesting:     false,
		OnlySyncCommittee: true,
	})
	selfIndices := make(map[phase0.ValidatorIndex]struct{}, len(selfSnapshots))
	for _, snapshot := range selfSnapshots {
		selfIndices[snapshot.Share.ValidatorIndex] = struct{}{}
	}

	storeDuties := make([]dutystore.StoreSyncCommitteeDuty, 0, len(duties))
	for _, duty := range duties {
		_, inCommittee := selfIndices[duty.ValidatorIndex]
		storeDuties = append(storeDuties, dutystore.StoreSyncCommitteeDuty{
			ValidatorIndex: duty.ValidatorIndex,
			Duty:           duty,
			InCommittee:    inCommittee,
		})
	}

	span.AddEvent("storing duties", trace.WithAttributes(observability.DutyCountAttribute(len(storeDuties))))
	h.duties.Set(period, storeDuties)

	h.prepareDutiesResultLog(period, duties, start)

	// lastEpoch + 1 due to the fact that we need to subscribe "until" the end of the period
	lastEpoch := h.beaconConfig.FirstEpochOfSyncPeriod(period+1) - 1
	subscriptions := calculateSubscriptions(lastEpoch+1, duties)

	if len(subscriptions) == 0 {
		span.AddEvent("no subscriptions available")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		const eventMsg = "failed to get context deadline"
		span.AddEvent(eventMsg)
		h.logger.Warn(eventMsg)
		span.SetStatus(codes.Ok, "")
		return nil
	}

	span.AddEvent("submitting beacon sync committee subscriptions", trace.WithAttributes(
		attribute.Int("ssv.validator.duty.subscriptions", len(subscriptions)),
	))
	go func(ctx context.Context, h *SyncCommitteeHandler, subscriptions []*eth2apiv1.SyncCommitteeSubscription) {
		subscriptionCtx, cancel := context.WithDeadline(ctx, deadline)
		defer cancel()

		if err := h.beaconNode.SubmitSyncCommitteeSubscriptions(subscriptionCtx, subscriptions); err != nil {
			h.logger.Warn("failed to subscribe sync committee to subnet", zap.Error(err))
		}
	}(ctx, h, subscriptions)

	span.SetStatus(codes.Ok, "")
	return nil
}

func (h *SyncCommitteeHandler) prepareDutiesResultLog(period uint64, duties []*eth2apiv1.SyncCommitteeDuty, start time.Time) {
	var b strings.Builder
	for i, duty := range duties {
		if i > 0 {
			b.WriteString(", ")
		}
		tmp := fmt.Sprintf("%v-p%v-v%v", h.Name(), period, duty.ValidatorIndex)
		b.WriteString(tmp)
	}
	h.logger.Debug("👥 got duties",
		fields.Count(len(duties)),
		zap.String("period", fmt.Sprintf("p%v", period)),
		zap.Any("duties", b.String()),
		fields.Duration(start))
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
	currentSlot := h.beaconConfig.EstimatedCurrentSlot()
	currentEpoch := h.beaconConfig.EstimatedEpochAtSlot(currentSlot)

	snapshot, exists := h.validatorProvider.GetValidator(registrystorage.ValidatorPubKey(duty.PubKey))
	if !exists {
		h.logger.Warn("validator not found", fields.Validator(duty.PubKey[:]))
		return false
	}
	v := &snapshot.Share

	if v.MinParticipationEpoch() > currentEpoch {
		h.logger.Debug("validator not yet participating",
			fields.Validator(duty.PubKey[:]),
			zap.Uint64("min_participation_epoch", uint64(v.MinParticipationEpoch())),
			zap.Uint64("current_epoch", uint64(currentEpoch)),
		)
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
	return h.beaconConfig.GetEpochsPerSyncCommitteePeriod() * h.beaconConfig.GetSlotsPerEpoch()
}
