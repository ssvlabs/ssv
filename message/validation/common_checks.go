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
	if earliness := mv.messageEarliness(messageSlot, receivedAt.Add(clockErrorTolerance)); earliness > 0 {
		e := ErrEarlyMessage
		e.got = fmt.Sprintf("early by %v", earliness)
		return e
	}

	if lateness := mv.messageLateness(messageSlot, role, receivedAt); lateness > clockErrorTolerance {
		e := ErrLateMessage
		e.got = fmt.Sprintf("late by %v", lateness)
		return e
	}

	return nil
}

// messageEarliness returns how early message is or 0 if it's not
func (mv *messageValidator) messageEarliness(slot phase0.Slot, receivedAt time.Time) time.Duration {
	slotEndTimeWithError := mv.netCfg.Beacon.GetSlotEndTime(mv.netCfg.Beacon.EstimatedSlotAtTime(receivedAt.Unix()))

	return mv.netCfg.Beacon.GetSlotStartTime(slot).Sub(slotEndTimeWithError)
}

// messageLateness returns how late message is or 0 if it's not
func (mv *messageValidator) messageLateness(slot phase0.Slot, role spectypes.RunnerRole, receivedAt time.Time) time.Duration {
	var ttl phase0.Slot
	switch role {
	case spectypes.RoleProposer, spectypes.RoleSyncCommitteeContribution:
		ttl = 1 + lateSlotAllowance
	case spectypes.RoleCommittee, spectypes.RoleAggregator:
		ttl = phase0.Slot(mv.netCfg.Beacon.SlotsPerEpoch())
	case spectypes.RoleValidatorRegistration, spectypes.RoleVoluntaryExit:
		return 0
	}

	deadline := mv.netCfg.Beacon.GetSlotStartTime(slot + ttl).
		Add(lateMessageMargin)

	return mv.netCfg.Beacon.GetSlotStartTime(mv.netCfg.Beacon.EstimatedSlotAtTime(receivedAt.Unix())).
		Sub(deadline)
}

func (mv *messageValidator) validateDutyCount(
	validatorIndices []phase0.ValidatorIndex,
	state SignerState,
	msgID spectypes.MessageID,
	newDutyInSameEpoch bool,
) error {
	var dutyLimit int

	switch msgID.GetRoleType() {
	case spectypes.RoleAggregator, spectypes.RoleValidatorRegistration, spectypes.RoleVoluntaryExit:
		dutyLimit = 2

	case spectypes.RoleCommittee:
		dutyLimit = 2 * len(validatorIndices)

	default:
		return nil
	}

	if sameSlot := !newDutyInSameEpoch; sameSlot {
		dutyLimit++
	}

	if state.EpochDuties >= dutyLimit {
		err := ErrTooManyDutiesPerEpoch
		err.got = fmt.Sprintf("%v (role %v)", state.EpochDuties, msgID.GetRoleType())
		err.want = fmt.Sprintf("less than %v", dutyLimit)
		return err
	}

	return nil
}
