package validation

import (
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func (mv *messageValidator) committeeRole(role spectypes.RunnerRole) bool {
	return role == spectypes.RoleCommittee
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
	return mv.netCfg.Beacon.GetSlotStartTime(slot).Sub(receivedAt)
}

// messageLateness returns how late message is or 0 if it's not
func (mv *messageValidator) messageLateness(slot phase0.Slot, role spectypes.RunnerRole, receivedAt time.Time) time.Duration {
	var ttl phase0.Slot
	switch role {
	case spectypes.RoleProposer, spectypes.RoleSyncCommitteeContribution:
		ttl = 1 + lateSlotAllowance
	case spectypes.RoleCommittee, spectypes.RoleAggregator:
		ttl = phase0.Slot(mv.netCfg.Beacon.SlotsPerEpoch()) + lateSlotAllowance
	case spectypes.RoleValidatorRegistration, spectypes.RoleVoluntaryExit:
		return 0
	}

	deadline := mv.netCfg.Beacon.GetSlotStartTime(slot + ttl).
		Add(lateMessageMargin)

	return receivedAt.Sub(deadline)
}

func (mv *messageValidator) validateDutyCount(
	msgID spectypes.MessageID,
	msgSlot phase0.Slot,
	validatorIndexCount int,
	signerStateBySlot *OperatorState,
) error {
	dutyCount := signerStateBySlot.DutyCount(mv.netCfg.Beacon.EstimatedEpochAtSlot(msgSlot))

	dutyLimit, exists := mv.dutyLimit(msgID, msgSlot, validatorIndexCount)
	if !exists {
		return nil
	}

	if dutyCount >= dutyLimit {
		err := ErrTooManyDutiesPerEpoch
		err.got = fmt.Sprintf("%v (role %v)", dutyCount, msgID.GetRoleType())
		err.want = fmt.Sprintf("less than %v", dutyLimit)
		return err
	}

	return nil
}

func (mv *messageValidator) dutyLimit(msgID spectypes.MessageID, slot phase0.Slot, validatorIndexCount int) (int, bool) {
	switch msgID.GetRoleType() {
	case spectypes.RoleVoluntaryExit:
		pk := phase0.BLSPubKey{}
		copy(pk[:], msgID.GetDutyExecutorID())

		return mv.dutyStore.VoluntaryExit.GetDutyCount(slot, pk), true

	case spectypes.RoleAggregator, spectypes.RoleValidatorRegistration:
		return 2, true

	case spectypes.RoleCommittee:
		return 2 * validatorIndexCount, true

	default:
		return 0, false
	}
}

func (mv *messageValidator) validateBeaconDuty(
	role spectypes.RunnerRole,
	slot phase0.Slot,
	indices []phase0.ValidatorIndex,
) error {
	if len(indices) != 1 {
		return ErrProposalSCCSeveralIndices
	}

	index := indices[0]

	// Rule: For a proposal duty message, we check if the validator is assigned to it
	if role == spectypes.RoleProposer {
		epoch := mv.netCfg.Beacon.EstimatedEpochAtSlot(slot)
		if mv.dutyStore.Proposer.ValidatorDuty(epoch, slot, index) == nil {
			return ErrNoDuty
		}
	}

	// Rule: For a sync committee aggregation duty message, we check if the validator is assigned to it
	if role == spectypes.RoleSyncCommitteeContribution {
		period := mv.netCfg.Beacon.EstimatedSyncCommitteePeriodAtEpoch(mv.netCfg.Beacon.EstimatedEpochAtSlot(slot))
		if mv.dutyStore.SyncCommittee.Duty(period, index) == nil {
			return ErrNoDuty
		}
	}

	return nil
}
