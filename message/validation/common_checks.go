package validation

import (
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func (mv *messageValidator) committeeRole(role spectypes.RunnerRole) bool {
	return role == spectypes.RoleCommittee || role == spectypes.RoleAggregatorCommittee
}

func (mv *messageValidator) validateSlotTime(messageSlot phase0.Slot, role spectypes.RunnerRole, receivedAt time.Time) error {
	if earliness := mv.messageEarliness(messageSlot, receivedAt); earliness > clockErrorTolerance {
		e := ErrEarlySlotMessage
		e.got = fmt.Sprintf("early by %v", earliness)
		return e
	}

	if lateness := mv.messageLateness(messageSlot, role, receivedAt); lateness > clockErrorTolerance {
		e := ErrLateSlotMessage
		e.got = fmt.Sprintf("late by %v", lateness)
		return e
	}

	return nil
}

// messageEarliness returns how early message is or 0 if it's not
func (mv *messageValidator) messageEarliness(slot phase0.Slot, receivedAt time.Time) time.Duration {
	return mv.netCfg.SlotStartTime(slot).Sub(receivedAt)
}

// messageLateness returns how late message is or 0 if it's not
func (mv *messageValidator) messageLateness(slot phase0.Slot, role spectypes.RunnerRole, receivedAt time.Time) time.Duration {
	var ttl uint64
	switch role {
	case spectypes.RoleProposer, spectypes.RoleSyncCommitteeContribution:
		ttl = 1 + LateSlotAllowance
	case spectypes.RoleCommittee, spectypes.RoleAggregatorCommittee, spectypes.RoleAggregator:
		ttl = mv.maxStoredSlots()
	case spectypes.RoleValidatorRegistration, spectypes.RoleVoluntaryExit:
		return 0
	}

	deadline := mv.netCfg.SlotStartTime(slot + phase0.Slot(ttl)).
		Add(lateMessageMargin)

	return receivedAt.Sub(deadline)
}

func (mv *messageValidator) validateDutyCount(
	msgID spectypes.MessageID,
	msgSlot phase0.Slot,
	validatorIndices []phase0.ValidatorIndex,
	signerStateBySlot *OperatorState,
) error {
	dutyCount := signerStateBySlot.DutyCount(mv.netCfg.EstimatedEpochAtSlot(msgSlot))

	dutyLimit, exists := mv.dutyLimit(msgID, msgSlot, validatorIndices)
	if !exists {
		return nil
	}

	// If no message has been observed for this slot yet, treat it as a new duty.
	// It will increment the duty count during state update after successful validation,
	// so we preemptively increment the checked duty count to reflect that.
	if signerStateBySlot.GetSignerState(msgSlot) == nil {
		dutyCount++
	}

	if dutyCount > dutyLimit {
		err := ErrTooManyDutiesPerEpoch
		err.got = fmt.Sprintf("%v (role %v)", dutyCount, msgID.GetRoleType())
		err.want = fmt.Sprintf("<=%v", dutyLimit)
		return err
	}

	return nil
}

func (mv *messageValidator) dutyLimit(msgID spectypes.MessageID, slot phase0.Slot, validatorIndices []phase0.ValidatorIndex) (uint64, bool) {
	switch msgID.GetRoleType() {
	case spectypes.RoleVoluntaryExit:
		pk := phase0.BLSPubKey{}
		copy(pk[:], msgID.GetDutyExecutorID())

		return mv.dutyStore.VoluntaryExit.GetDutyCount(slot, pk), true

	case spectypes.RoleAggregator, spectypes.RoleValidatorRegistration:
		return 2, true

	case spectypes.RoleCommittee, spectypes.RoleAggregatorCommittee:
		validatorIndexCount := uint64(len(validatorIndices))
		slotsPerEpoch := mv.netCfg.SlotsPerEpoch

		// Skip duty search if validators * 2 exceeds slots per epoch,
		// as the maximum duties per epoch is capped at the number of slots.
		// This avoids unnecessary checks.
		if validatorIndexCount < slotsPerEpoch/2 {
			// Check if there is at least one validator in the sync committee.
			// If so, the duty limit is equal to the number of slots per epoch.
			period := mv.netCfg.EstimatedSyncCommitteePeriodAtEpoch(mv.netCfg.EstimatedEpochAtSlot(slot))
			for _, i := range validatorIndices {
				if mv.dutyStore.SyncCommittee.Duty(period, i) != nil {
					return slotsPerEpoch, true
				}
			}
		}

		return min(slotsPerEpoch, 2*validatorIndexCount), true

	default:
		return 0, false
	}
}

func (mv *messageValidator) validateBeaconDuty(
	role spectypes.RunnerRole,
	slot phase0.Slot,
	indices []phase0.ValidatorIndex,
	randaoMsg bool,
) error {
	epoch := mv.netCfg.EstimatedEpochAtSlot(slot)

	// Rule: For a proposal duty message, we check if the validator is assigned to it
	if role == spectypes.RoleProposer {
		// Tolerate missing duties for RANDAO signatures during the first slot of an epoch,
		// while duties are still being fetched from the Beacon node.
		//
		// Note: we allow current slot to be lower because of the ErrEarlyMessage rule.
		if randaoMsg && mv.netCfg.IsFirstSlotOfEpoch(slot) && mv.netCfg.EstimatedCurrentSlot() <= slot {
			if !mv.dutyStore.Proposer.IsEpochSet(epoch) {
				return nil
			}
		}

		// Non-committee roles always have one validator index.
		validatorIndex := indices[0]
		if mv.dutyStore.Proposer.ValidatorDuty(epoch, slot, validatorIndex) == nil {
			return ErrNoDuty
		}
	}

	// Rule: For a sync committee aggregation duty message, we check if the validator is assigned to it
	if role == spectypes.RoleSyncCommitteeContribution {
		period := mv.netCfg.EstimatedSyncCommitteePeriodAtEpoch(epoch)
		// Non-committee roles always have one validator index.
		validatorIndex := indices[0]
		if mv.dutyStore.SyncCommittee.Duty(period, validatorIndex) == nil {
			return ErrNoDuty
		}
	}

	return nil
}
