package duties

import (
	"context"
	"fmt"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

type committeeDutiesMap map[spectypes.CommitteeID]*committeeDuty

type CommitteeHandler struct {
	baseHandler

	attDuties  *dutystore.Duties[eth2apiv1.AttesterDuty]
	syncDuties *dutystore.SyncCommitteeDuties
}

type committeeDuty struct {
	duty        *spectypes.CommitteeDuty
	id          spectypes.CommitteeID
	operatorIDs []spectypes.OperatorID
}

func NewCommitteeHandler(attDuties *dutystore.Duties[eth2apiv1.AttesterDuty], syncDuties *dutystore.SyncCommitteeDuties) *CommitteeHandler {
	h := &CommitteeHandler{
		attDuties:  attDuties,
		syncDuties: syncDuties,
	}

	return h
}

func (h *CommitteeHandler) Name() string {
	return "CLUSTER"
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
			epoch := h.beaconConfig.EstimatedEpochAtSlot(slot)
			period := h.beaconConfig.EstimatedSyncCommitteePeriodAtEpoch(epoch)
			buildStr := fmt.Sprintf("p%v-e%v-s%v-#%v", period, epoch, slot, slot%32+1)

			h.logger.Debug("🛠 ticker event", zap.String("period_epoch_slot_pos", buildStr))
			h.processExecution(ctx, period, epoch, slot)

		case <-h.reorg:
			h.logger.Debug("🛠 reorg event")

		case <-h.indicesChange:
			h.logger.Debug("🛠 indicesChange event")
		}
	}
}

func (h *CommitteeHandler) processExecution(ctx context.Context, period uint64, epoch phase0.Epoch, slot phase0.Slot) {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "committee.execute"),
		trace.WithAttributes(
			observability.BeaconSlotAttribute(slot),
			observability.BeaconEpochAttribute(epoch),
			observability.BeaconPeriodAttribute(period),
		))
	defer span.End()

	attDuties := h.attDuties.CommitteeSlotDuties(epoch, slot)
	syncDuties := h.syncDuties.CommitteePeriodDuties(period)
	if attDuties == nil && syncDuties == nil {
		const eventMsg = "no attester or sync-committee duties to execute"
		h.logger.Debug(eventMsg, fields.Epoch(epoch), fields.Slot(slot))
		span.AddEvent(eventMsg)
		span.SetStatus(codes.Ok, "")
		return
	}

	committeeMap := h.buildCommitteeDuties(attDuties, syncDuties, epoch, slot)
	if len(committeeMap) == 0 {
		h.logger.Debug("no committee duties to execute", fields.Epoch(epoch), fields.Slot(slot))
	}

	h.dutiesExecutor.ExecuteCommitteeDuties(ctx, committeeMap)

	span.SetStatus(codes.Ok, "")
}

func (h *CommitteeHandler) buildCommitteeDuties(
	attDuties []*eth2apiv1.AttesterDuty,
	syncDuties []*eth2apiv1.SyncCommitteeDuty,
	epoch phase0.Epoch,
	slot phase0.Slot,
) committeeDutiesMap {
	// NOTE: Instead of getting validators using duties one by one, we are getting all validators for the slot at once.
	// This approach reduces contention and improves performance, as multiple individual calls would be slower.
	selfValidators := h.validatorProvider.SelfParticipatingValidators(epoch)

	validatorCommittees := map[phase0.ValidatorIndex]committeeDuty{}
	for _, validatorShare := range selfValidators {
		cd := committeeDuty{
			id:          validatorShare.CommitteeID(),
			operatorIDs: validatorShare.OperatorIDs(),
		}
		validatorCommittees[validatorShare.ValidatorIndex] = cd
	}

	resultCommitteeMap := make(committeeDutiesMap)
	for _, duty := range attDuties {
		if h.shouldExecuteAtt(duty, epoch) {
			h.addToCommitteeMap(resultCommitteeMap, validatorCommittees, h.toSpecAttDuty(duty, spectypes.BNRoleAttester))
		}
	}
	for _, duty := range syncDuties {
		if h.shouldExecuteSync(duty, slot, epoch) {
			h.addToCommitteeMap(resultCommitteeMap, validatorCommittees, h.toSpecSyncDuty(duty, slot, spectypes.BNRoleSyncCommittee))
		}
	}

	return resultCommitteeMap
}

func (h *CommitteeHandler) addToCommitteeMap(
	committeeDutyMap committeeDutiesMap,
	validatorCommittees map[phase0.ValidatorIndex]committeeDuty,
	specDuty *spectypes.ValidatorDuty,
) {
	committee, ok := validatorCommittees[specDuty.ValidatorIndex]
	if !ok {
		h.logger.Error("failed to find committee for validator", zap.Uint64("validator_index", uint64(specDuty.ValidatorIndex)))
		return
	}

	cd, exists := committeeDutyMap[committee.id]
	if !exists {
		cd = &committeeDuty{
			id:          committee.id,
			operatorIDs: committee.operatorIDs,
			duty: &spectypes.CommitteeDuty{
				Slot:            specDuty.Slot,
				ValidatorDuties: []*spectypes.ValidatorDuty{},
			},
		}

		committeeDutyMap[committee.id] = cd
	}

	cd.duty.ValidatorDuties = append(cd.duty.ValidatorDuties, specDuty)
}

func (h *CommitteeHandler) toSpecAttDuty(duty *eth2apiv1.AttesterDuty, role spectypes.BeaconRole) *spectypes.ValidatorDuty {
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

func (h *CommitteeHandler) toSpecSyncDuty(duty *eth2apiv1.SyncCommitteeDuty, slot phase0.Slot, role spectypes.BeaconRole) *spectypes.ValidatorDuty {
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

func (h *CommitteeHandler) shouldExecuteAtt(duty *eth2apiv1.AttesterDuty, epoch phase0.Epoch) bool {
	share, found := h.validatorProvider.Validator(duty.PubKey[:])
	if !found || !share.IsParticipatingAndAttesting(epoch) {
		return false
	}

	currentSlot := h.beaconConfig.EstimatedCurrentSlot()

	if participates := h.canParticipate(share, currentSlot); !participates {
		return false
	}

	// execute task if slot already began and not pass 1 epoch
	var attestationPropagationSlotRange = h.beaconConfig.GetSlotsPerEpoch()
	if currentSlot >= duty.Slot && uint64(currentSlot-duty.Slot) <= attestationPropagationSlotRange {
		return true
	}
	if currentSlot+1 == duty.Slot {
		h.warnMisalignedSlotAndDuty(duty.String())
		return true
	}

	return false
}

func (h *CommitteeHandler) shouldExecuteSync(duty *eth2apiv1.SyncCommitteeDuty, slot phase0.Slot, epoch phase0.Epoch) bool {
	share, found := h.validatorProvider.Validator(duty.PubKey[:])
	if !found || !share.IsParticipating(h.beaconConfig, epoch) {
		return false
	}

	currentSlot := h.beaconConfig.EstimatedCurrentSlot()

	if participates := h.canParticipate(share, currentSlot); !participates {
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

func (h *CommitteeHandler) canParticipate(share *types.SSVShare, currentSlot phase0.Slot) bool {
	currentEpoch := h.beaconConfig.EstimatedEpochAtSlot(currentSlot)

	if share.MinParticipationEpoch() > currentEpoch {
		h.logger.Debug("validator not yet participating",
			fields.Validator(share.ValidatorPubKey[:]),
			zap.Uint64("min_participation_epoch", uint64(share.MinParticipationEpoch())),
			zap.Uint64("current_epoch", uint64(currentEpoch)),
		)
		return false
	}

	return true
}
