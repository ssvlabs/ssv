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

func (mv *messageValidator) validateSlotTime(messageSlot phase0.Slot, role spectypes.RunnerRole, receivedAt time.Time) error {
	if earliness := mv.messageEarliness(messageSlot, receivedAt); earliness > clockErrorTolerance+mv.earlySlotAllowance(role) {
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

// earlySlotAllowance returns how far ahead of its slot a message for the role may legitimately
// arrive. Proposer preferences are broadcast across the proposer lookahead — the current epoch plus
// MIN_SEED_LOOKAHEAD — so their proposal-slot messages are expected up to that far in the future;
// every other role acts at (or after) its slot, so the default is none.
func (mv *messageValidator) earlySlotAllowance(role spectypes.RunnerRole) time.Duration {
	if role == spectypes.RoleProposerPreferences {
		// #nosec G115 -- a small epoch count times slots-per-epoch cannot overflow int64.
		return time.Duration(proposerPreferencesEarlyEpochs*mv.netCfg.SlotsPerEpoch) * mv.netCfg.SlotDuration
	}
	return 0
}

// messageLateness returns how late message is or 0 if it's not
func (mv *messageValidator) messageLateness(slot phase0.Slot, role spectypes.RunnerRole, receivedAt time.Time) time.Duration {
	var ttl uint64
	switch role {
	case spectypes.RoleProposer, spectypes.RoleEnvelopeBuilder, spectypes.RolePTCAttester, ssvtypes.RoleSyncCommitteeContribution:
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
	// - SlotsPerEpoch for proposer preferences and self-build envelopes
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

	case spectypes.RoleProposerPreferences, spectypes.RoleEnvelopeBuilder:
		// A validator proposes at most once per slot, so at most SlotsPerEpoch preferences (and likewise
		// self-build envelopes) per epoch.
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

	// Rule: For a proposer-preferences message, require a real proposer assignment for the validator at
	// the slot — but only once the slot's epoch is fetched. Preferences ride a future proposal slot
	// whose epoch may still be in flight; tolerate that (the earliness/lateness window bounds the slot).
	if role == spectypes.RoleProposerPreferences {
		validatorIndex := indices[0]
		if mv.dutyStore.Proposer.IsEpochSet(epoch) && mv.dutyStore.Proposer.ValidatorDuty(epoch, slot, validatorIndex) == nil {
			return ErrNoDuty
		}
	}

	// The self-build envelope rides the proposer's slot, so it must carry a real proposer assignment —
	// guarded by IsEpochSet like proposer-preferences, since the message can arrive before the epoch's
	// duties are fetched.
	if role == spectypes.RoleEnvelopeBuilder {
		validatorIndex := indices[0]
		if mv.dutyStore.Proposer.IsEpochSet(epoch) && mv.dutyStore.Proposer.ValidatorDuty(epoch, slot, validatorIndex) == nil {
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
	// epoch (e.g. at startup) must be tolerated rather than rejected.
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
