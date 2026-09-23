package validation

import (
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

func (mv *messageValidator) committeeRole(role spectypes.RunnerRole) bool {
	return role == spectypes.RoleCommittee || role == spectypes.RoleAggregatorCommittee
}

// monotonicSlotRole reports whether a role's signer advances through slots one at a time, so a message
// for a slot below the signer's max is stale and must be rejected. False for committee roles (state is
// slot-keyed across many validators) and for proposer preferences (a signer holds its whole lookahead
// of proposal slots at once, so a lower slot is a concurrent duty, not a stale one — its replay bound
// is the earliness/lateness window instead).
func (mv *messageValidator) monotonicSlotRole(role spectypes.RunnerRole) bool {
	return !mv.committeeRole(role) && role != spectypes.RoleProposerPreferences
}

// validateSlotTime bounds a message's arrival against its slot: no earlier than clockErrorTolerance plus
// earlyMessageMargin before the moment a message for the slot is expected (see messageEarliness), and no
// later than the role's TTL plus lateMessageMargin (see messageLateness).
func (mv *messageValidator) validateSlotTime(messageSlot phase0.Slot, role spectypes.RunnerRole, receivedAt time.Time) error {
	if earliness := mv.messageEarliness(messageSlot, role, receivedAt); earliness > clockErrorTolerance+earlyMessageMargin {
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

// messageEarliness returns how early the message is, or 0 if it is not: the time from its arrival to the
// earliest moment a message for its slot is expected. That is the slot's own start for every role that acts
// at or after its slot. Proposer preferences are broadcast across the proposer lookahead — the current epoch
// and the next (MIN_SEED_LOOKAHEAD) — so one for a slot in epoch E is expected from the start of epoch E-1;
// one further out is early: no honest operator holds a proposer assignment that far ahead, and the proposer
// gate in validateBeaconDuty could not check it against duties not fetched yet.
func (mv *messageValidator) messageEarliness(slot phase0.Slot, role spectypes.RunnerRole, receivedAt time.Time) time.Duration {
	expectedFrom := mv.netCfg.SlotStartTime(slot)
	if role == spectypes.RoleProposerPreferences {
		epoch := mv.netCfg.EstimatedEpochAtSlot(slot)
		const lookback = phase0.Epoch(proposerPreferencesEarlyEpochs - 1)
		if epoch >= lookback {
			epoch -= lookback
		} else {
			epoch = 0
		}
		expectedFrom = mv.netCfg.SlotStartTime(mv.netCfg.FirstSlotAtEpoch(epoch))
	}
	return expectedFrom.Sub(receivedAt)
}

// messageLateness returns how late message is or 0 if it's not
func (mv *messageValidator) messageLateness(slot phase0.Slot, role spectypes.RunnerRole, receivedAt time.Time) time.Duration {
	var ttl uint64
	switch role {
	case spectypes.RoleProposer, spectypes.RolePTCAttester, ssvtypes.RoleSyncCommitteeContribution:
		ttl = 1 + LateSlotAllowance
	case spectypes.RoleCommittee, spectypes.RoleAggregatorCommittee, ssvtypes.RoleAggregator:
		ttl = mv.maxStoredSlots()
	case spectypes.RoleValidatorRegistration, spectypes.RoleVoluntaryExit:
		// Deliberately exempt from the lateness bound: these duties aren't tied to a slot
		// deadline, so only the early-message check and per-epoch duty limits apply.
		return 0
	case spectypes.RoleProposerPreferences:
		// Preferences are consumed before their proposal slot; allow only a small grace past it so a
		// preference for a slot already behind us is rejected as a replay. This is the role's past
		// bound, since it is exempt from the monotonic slot-advance check.
		ttl = LateSlotAllowance
	default:
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
	operatorState *OperatorState,
) error {
	dutyCount := operatorState.DutyCount(mv.netCfg.EstimatedEpochAtSlot(msgSlot))

	dutyLimit, exists := mv.dutyLimit(msgID, msgSlot, validatorIndices)
	if !exists {
		return nil
	}

	// If no message has been observed for this slot yet, treat it as a new duty.
	// It will increment the duty count during state update after successful validation,
	// so we preemptively increment the checked duty count to reflect that.
	if operatorState.GetSignerStateForSlot(msgSlot) == nil {
		dutyCount++
	}

	// Rule: valid number of duties per epoch:
	// - 2 for aggregation, validator registration and PTC attestation
	// - the tracked exit-duty count for voluntary exit
	// - 2*V for Committee and AggregatorCommittee duty (where V is the number of validators in the cluster) (if no validator is doing sync committee in this epoch)
	// - SlotsPerEpoch for proposer preferences
	// - else, accept
	if dutyCount > dutyLimit {
		e := ErrTooManyDutiesPerEpoch
		e.got = fmt.Sprintf("%v (role %v)", dutyCount, msgID.GetRoleType())
		e.want = fmt.Sprintf("<=%v", dutyLimit)
		return e
	}

	return nil
}

func (mv *messageValidator) dutyLimit(msgID spectypes.MessageID, slot phase0.Slot, validatorIndices []phase0.ValidatorIndex) (uint64, bool) {
	switch msgID.GetRoleType() {
	case spectypes.RoleVoluntaryExit:
		pk := phase0.BLSPubKey{}
		copy(pk[:], msgID.GetDutyExecutorID())

		return mv.dutyStore.VoluntaryExit.GetDutyCount(slot, pk), true

	case ssvtypes.RoleAggregator, spectypes.RoleValidatorRegistration, spectypes.RolePTCAttester:
		// 2 = one duty per epoch plus a reorg margin. A PTC member is drawn from a beacon committee, and a
		// validator sits on exactly one beacon committee per epoch, so it signs at most one payload
		// attestation per epoch — the same bound as aggregation and validator registration.
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

	case spectypes.RoleProposerPreferences:
		// A validator proposes at most once per slot, so at most SlotsPerEpoch preferences per epoch.
		return mv.netCfg.SlotsPerEpoch, true

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

	// The non-committee role checks below index indices[0]; reject a message carrying no validator
	// indices (every duty has at least one validator).
	if len(indices) == 0 {
		return ErrNoValidators
	}

	// Rule: For a proposal duty message, we check if the validator is assigned to it
	if role == spectypes.RoleProposer {
		// Tolerate missing duties for RANDAO signatures during the first slot of an epoch,
		// while duties are still being fetched from the Beacon node.
		//
		// Note: we allow current slot to be lower because of the early-message margin (ErrEarlySlotMessage).
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

	// Rule: For a proposer-preferences message, require a real proposer assignment for the validator at
	// the slot — but only from a fetched AND fresh epoch. It rides a proposal slot whose epoch may still
	// be in flight (tolerated; the earliness/lateness window bounds the slot), and an epoch fetched before
	// the latest indices change is equally unusable for rejection: dropping a just-added validator's
	// one-shot partial on a stale view starves its quorum permanently — an identical re-broadcast can't
	// pass the gossip seen-cache (SIP #94 §5, §7).
	if role == spectypes.RoleProposerPreferences {
		validatorIndex := indices[0]
		if mv.dutyStore.Proposer.IsEpochSet(epoch) && !mv.dutyStore.Proposer.IsEpochStale(epoch) &&
			mv.dutyStore.Proposer.ValidatorDuty(epoch, slot, validatorIndex) == nil {
			return ErrNoDuty
		}
	}

	// Rule: For a sync committee aggregation duty message, we check if the validator is assigned to it
	if role == ssvtypes.RoleSyncCommitteeContribution {
		period := mv.netCfg.EstimatedSyncCommitteePeriodAtEpoch(epoch)
		// Non-committee roles always have one validator index.
		validatorIndex := indices[0]
		if mv.dutyStore.SyncCommittee.Duty(period, validatorIndex) == nil {
			return ErrNoDuty
		}
	}

	// Rule: For a PTC attestation message, require a real PTC assignment for the validator at the slot,
	// but only once the slot's epoch is fetched — PTC duties are fetched per epoch, so a not-yet-fetched
	// epoch (e.g. at startup) must be tolerated rather than rejected. A validator-set change clears the
	// PTC store outright (SIP #94 §3: the refetch replaces the cache rather than merging into it), so until
	// the next tick refetches — one slot at most — the epoch reads as not yet fetched and is tolerated the
	// same way: the tolerance the proposer-preferences branch above takes from IsEpochStale, reached here
	// through the store's own replace semantics.
	if role == spectypes.RolePTCAttester {
		// Non-committee roles always have one validator index.
		validatorIndex := indices[0]
		if mv.dutyStore.PTC.IsEpochSet(epoch) && mv.dutyStore.PTC.ValidatorDuty(epoch, slot, validatorIndex) == nil {
			return ErrNoDuty
		}
	}

	// Committee roles (RoleCommittee and RoleAggregatorCommittee) are intentionally not
	// per-validator duty-asserted here. As elsewhere in committee-role validation, we do not assume
	// operators are synced on each other's validator sets (see knowledge-base#2), so asserting a
	// per-validator attester/sync-committee duty would reject legitimate messages from nodes still
	// mid-sync — the self-reinforcing failure mode kb#2 documents. The pre-Boole
	// RoleSyncCommitteeContribution branch above has the assertion only because it was a
	// per-validator (non-committee) role with a single known index; RoleAggregatorCommittee carries
	// that traffic post-fork as a committee role, so the assertion is dropped by design. The residual
	// spam is insider-only (the signer is an authenticated committee member) and bounded by the
	// per-epoch duty-count limit for committee roles in dutyLimit (min(slotsPerEpoch, 2*validators)).
	return nil
}
