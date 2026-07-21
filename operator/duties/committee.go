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
	"github.com/ssvlabs/ssv/observability/utils"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

type committeeDutiesMap map[spectypes.CommitteeID]*committeeDuty

type CommitteeHandler struct {
	baseHandler

	attDuties  *dutystore.Duties[eth2apiv1.AttesterDuty]
	syncDuties *dutystore.SyncCommitteeDuties

	isAggregator bool
}

type committeeDuty struct {
	duty        spectypes.Duty
	id          spectypes.CommitteeID
	operatorIDs []spectypes.OperatorID
}

func (cd *committeeDuty) validatorDuties() []*spectypes.ValidatorDuty {
	switch duty := cd.duty.(type) {
	case *spectypes.CommitteeDuty:
		return duty.ValidatorDuties
	case *spectypes.AggregatorCommitteeDuty:
		return duty.ValidatorDuties
	default:
		return nil
	}
}

func (cd *committeeDuty) appendValidatorDuty(duty *spectypes.ValidatorDuty) {
	switch target := cd.duty.(type) {
	case *spectypes.CommitteeDuty:
		target.ValidatorDuties = append(target.ValidatorDuties, duty)
	case *spectypes.AggregatorCommitteeDuty:
		target.ValidatorDuties = append(target.ValidatorDuties, duty)
	}
}

func (h *CommitteeHandler) newCommitteeDuty(slot phase0.Slot) spectypes.Duty {
	if h.isAggregator {
		return &spectypes.AggregatorCommitteeDuty{
			Slot:            slot,
			ValidatorDuties: []*spectypes.ValidatorDuty{},
		}
	}
	return &spectypes.CommitteeDuty{
		Slot:            slot,
		ValidatorDuties: []*spectypes.ValidatorDuty{},
	}
}

// NewCommitteeHandler creates a handler for either the committee (isAggregator=false) or the
// aggregator-committee (isAggregator=true) duty flavor; the two differ only in this flag.
func NewCommitteeHandler(
	attDuties *dutystore.Duties[eth2apiv1.AttesterDuty],
	syncDuties *dutystore.SyncCommitteeDuties,
	isAggregator bool,
) *CommitteeHandler {
	return &CommitteeHandler{
		isAggregator: isAggregator,
		attDuties:    attDuties,
		syncDuties:   syncDuties,
	}
}

func (h *CommitteeHandler) Name() string {
	if h.isAggregator {
		return utils.FormatRunnerRole(spectypes.RoleAggregatorCommittee)
	}
	return utils.FormatRunnerRole(spectypes.RoleCommittee)
}

func (h *CommitteeHandler) WaitShutdown() {}

func (h *CommitteeHandler) HandleDuties(ctx context.Context) {
	h.logger.Info("starting duty handler")
	defer h.logger.Info("duty handler exited")

	next := h.ticker.Next()
	for {
		select {
		case <-ctx.Done():
			return

		case <-next:
			currentSlot := h.ticker.Slot()
			next = h.ticker.Next()

			if h.isAggregator && !h.netCfg.BooleForkAtSlot(currentSlot) {
				continue
			}

			currentEpoch := h.netCfg.EstimatedEpochAtSlot(currentSlot)
			currentPeriod := h.netCfg.EstimatedSyncCommitteePeriodAtEpoch(currentEpoch)

			slotNumber := uint64(currentSlot)%h.netCfg.SlotsPerEpoch + 1
			buildStr := fmt.Sprintf("p%v-e%v-s%v-#%v", currentPeriod, currentEpoch, currentSlot, slotNumber)
			h.logger.Debug("🛠 ticker event", zap.String("period_epoch_slot_pos", buildStr))

			func() {
				// tickCtx ensures we never take too long to process ticks (otherwise we might not be able to catch up
				// with the latest tick for a while, if ever). Since the ticker always fires at around slot start-time,
				// setting the deadline to currentSlot+1 gives us about ~1 full slot (12s) to process the tick.
				tickCtx, cancel := context.WithDeadline(ctx, h.netCfg.SlotStartTime(currentSlot+1))
				defer cancel()

				h.processExecution(tickCtx, currentPeriod, currentEpoch, currentSlot)
			}()

		case <-h.reorgEventsCh:
			h.logger.Debug("🛠 reorg event")

		case <-h.indicesChangeCh:
			h.logger.Debug("🛠 indicesChange event")
		}
	}
}

func (h *CommitteeHandler) processExecution(ctx context.Context, period uint64, epoch phase0.Epoch, slot phase0.Slot) {
	spanName := "committee.execute"
	if h.isAggregator {
		spanName = "aggregator_committee.execute"
	}
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, spanName),
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

	// Attestation and aggregation submissions are rewarded as long as they are included within
	// SLOTS_PER_EPOCH slots of their target slot (i.e., from target slot up to and including target + SLOTS_PER_EPOCH).
	// See https://eth2book.info/latest/part2/incentives/rewards/#attestation-rewards
	// Sync committee duties have to use the same deadline because they are part of the committee role.
	// We set the deadline to target slot + SLOTS_PER_EPOCH + 1 (since the deadline slot itself is excluded).
	slotsPerEpoch := phase0.Slot(h.netCfg.SlotsPerEpoch)
	dutyDeadline := h.netCfg.SlotStartTime(slot + slotsPerEpoch + 1)
	h.dutiesExecutor.ExecuteCommitteeDuties(ctx, committeeMap, dutyDeadline)

	span.SetStatus(codes.Ok, "")
}

func (h *CommitteeHandler) buildCommitteeDuties(
	attDuties []*eth2apiv1.AttesterDuty,
	syncDuties []*eth2apiv1.SyncCommitteeDuty,
	epoch phase0.Epoch,
	slot phase0.Slot,
) committeeDutiesMap {
	attRole := spectypes.BNRoleAttester
	syncRole := spectypes.BNRoleSyncCommittee
	if h.isAggregator {
		attRole = spectypes.BNRoleAggregator
		syncRole = spectypes.BNRoleSyncCommitteeContribution
	}

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
			h.addToCommitteeMap(resultCommitteeMap, validatorCommittees, h.toSpecAttDuty(duty, attRole))
		}
	}
	for _, duty := range syncDuties {
		if h.shouldExecuteSync(duty, slot, epoch) {
			h.addToCommitteeMap(resultCommitteeMap, validatorCommittees, h.toSpecSyncDuty(duty, slot, syncRole))
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
			duty:        h.newCommitteeDuty(specDuty.Slot),
		}

		committeeDutyMap[committee.id] = cd
	}

	cd.appendValidatorDuty(specDuty)
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
	if !found || !share.IsAttesting(epoch) {
		return false
	}

	currentSlot := h.netCfg.EstimatedCurrentSlot()

	if participates := h.canParticipate(share, currentSlot); !participates {
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

func (h *CommitteeHandler) shouldExecuteSync(duty *eth2apiv1.SyncCommitteeDuty, slot phase0.Slot, epoch phase0.Epoch) bool {
	share, found := h.validatorProvider.Validator(duty.PubKey[:])
	if !found || !share.IsParticipating(h.netCfg.Beacon, epoch) {
		return false
	}

	currentSlot := h.netCfg.EstimatedCurrentSlot()

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
	currentEpoch := h.netCfg.EstimatedEpochAtSlot(currentSlot)

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
