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

	duties     *dutystore.Duties[eth2apiv1.ProposerDuty]
	fetchFirst bool
}

func NewProposerHandler(duties *dutystore.Duties[eth2apiv1.ProposerDuty]) *ProposerHandler {
	return &ProposerHandler{
		duties:     duties,
		fetchFirst: true,
	}
}

func (h *ProposerHandler) Name() string {
	return spectypes.BNRoleProposer.String()
}

// HandleDuties manages the duty lifecycle, handling different cases:
//
// On First Run:
//  1. Fetch duties for the current epoch.
//  2. Execute duties.
//
// On Re-org (current dependent root changed):
//  1. Fetch duties for the current epoch.
//  2. Execute duties.
//
// On Indices Change:
//  1. Execute duties.
//  2. ResetEpoch duties for the current epoch.
//  3. Fetch duties for the current epoch.
//
// On Ticker event:
//  1. Execute duties.
//  2. If necessary, fetch duties for the current epoch.
func (h *ProposerHandler) HandleDuties(ctx context.Context) {
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
			currentEpoch := h.beaconConfig.EstimatedEpochAtSlot(slot)
			buildStr := fmt.Sprintf("e%v-s%v-#%v", currentEpoch, slot, slot%32+1)
			h.logger.Debug("🛠 ticker event", zap.String("epoch_slot_pos", buildStr))

			func() {
				tickCtx, cancel := context.WithDeadline(ctx, h.beaconConfig.SlotStartTime(slot+1).Add(100*time.Millisecond))
				defer cancel()

				if h.fetchFirst {
					h.fetchFirst = false
					h.indicesChanged = false
					h.processFetching(tickCtx, currentEpoch)
					h.processExecution(tickCtx, currentEpoch, slot)
				} else {
					h.processExecution(tickCtx, currentEpoch, slot)
					if h.indicesChanged {
						h.indicesChanged = false
						h.processFetching(tickCtx, currentEpoch)
					}
				}
			}()

			// last slot of epoch
			if uint64(slot)%h.beaconConfig.SlotsPerEpoch == h.beaconConfig.SlotsPerEpoch-1 {
				h.duties.ResetEpoch(currentEpoch - 1)
				h.fetchFirst = true
			}

		case reorgEvent := <-h.reorg:
			currentEpoch := h.beaconConfig.EstimatedEpochAtSlot(reorgEvent.Slot)
			buildStr := fmt.Sprintf("e%v-s%v-#%v", currentEpoch, reorgEvent.Slot, reorgEvent.Slot%32+1)
			h.logger.Info("🔀 reorg event received", zap.String("epoch_slot_pos", buildStr), zap.Any("event", reorgEvent))

			// reset current epoch duties
			if reorgEvent.Current {
				h.duties.ResetEpoch(currentEpoch)
				h.fetchFirst = true
			}

		case <-h.indicesChange:
			slot := h.beaconConfig.EstimatedCurrentSlot()
			currentEpoch := h.beaconConfig.EstimatedEpochAtSlot(slot)
			buildStr := fmt.Sprintf("e%v-s%v-#%v", currentEpoch, slot, slot%32+1)
			h.logger.Info("🔁 indices change received", zap.String("epoch_slot_pos", buildStr))

			h.indicesChanged = true
		}
	}
}

func (h *ProposerHandler) HandleInitialDuties(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, h.beaconConfig.SlotDuration/2)
	defer cancel()

	epoch := h.beaconConfig.EstimatedCurrentEpoch()
	h.processFetching(ctx, epoch)
}

func (h *ProposerHandler) processFetching(ctx context.Context, epoch phase0.Epoch) {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "proposer.fetch"),
		trace.WithAttributes(
			observability.BeaconEpochAttribute(epoch),
			observability.BeaconRoleAttribute(spectypes.BNRoleProposer),
		))
	defer span.End()

	span.AddEvent("fetching duties")
	if err := h.fetchAndProcessDuties(ctx, epoch); err != nil {
		// Set empty duties to inform DutyStore that fetch for this epoch is done.
		h.duties.Set(epoch, []dutystore.StoreDuty[eth2apiv1.ProposerDuty]{})
		h.logger.Error("failed to fetch duties for current epoch", zap.Error(err))
		span.SetStatus(codes.Error, err.Error())
		return
	}

	span.SetStatus(codes.Ok, "")
}

func (h *ProposerHandler) processExecution(ctx context.Context, epoch phase0.Epoch, slot phase0.Slot) {
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

	h.dutiesExecutor.ExecuteDuties(ctx, toExecute)

	span.SetStatus(codes.Ok, "")
}

func (h *ProposerHandler) fetchAndProcessDuties(ctx context.Context, epoch phase0.Epoch) error {
	start := time.Now()
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "proposer.fetch_and_store"),
		trace.WithAttributes(
			observability.BeaconEpochAttribute(epoch),
			observability.BeaconRoleAttribute(spectypes.BNRoleProposer),
		))
	defer span.End()

	var allEligibleIndices []phase0.ValidatorIndex
	for _, share := range h.validatorProvider.Validators() {
		if share.IsAttesting(epoch) {
			allEligibleIndices = append(allEligibleIndices, share.ValidatorIndex)
		}
	}
	if len(allEligibleIndices) == 0 {
		const eventMsg = "no eligible validators for epoch"
		h.logger.Debug(eventMsg, fields.Epoch(epoch))
		span.AddEvent(eventMsg)
		span.SetStatus(codes.Ok, "")
		return nil
	}

	selfEligibleIndices := map[phase0.ValidatorIndex]struct{}{}
	for _, share := range h.validatorProvider.SelfValidators() {
		if share.IsAttesting(epoch) {
			selfEligibleIndices[share.ValidatorIndex] = struct{}{}
		}
	}

	span.AddEvent("fetching duties from beacon node", trace.WithAttributes(observability.ValidatorCountAttribute(len(allEligibleIndices))))
	duties, err := h.beaconNode.ProposerDuties(ctx, epoch, allEligibleIndices)
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
	h.duties.Set(epoch, storeDuties)

	h.logger.Debug("📚 got duties",
		fields.Count(len(duties)),
		fields.Epoch(epoch),
		fields.Duties(epoch, specDuties),
		fields.Duration(start))

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
