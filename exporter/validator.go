package exporter

import (
	"errors"
	"fmt"
	"slices"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/hashicorp/go-multierror"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter/rolemask"
	"github.com/ssvlabs/ssv/exporter/traces"
	"github.com/ssvlabs/ssv/observability/log/fields"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// ErrPostForkCommitteeDutyNote marks the non-fatal note appended when a
// fork-straddling request reaches a post-fork slot/role pair without
// pubkeys/indices. It lets callers (e.g. the HTTP layer) tell this expected,
// partial-coverage note apart from genuine processing failures.
var ErrPostForkCommitteeDutyNote = errors.New("committee duty post-fork")

// committeeDutyFilterHint is the actionable tail shared by the committee-duty
// validation error and the post-fork partial-coverage note, hoisted so the two
// messages cannot drift apart.
const committeeDutyFilterHint = "please provide either pubkeys or indices to filter the duty for a specific validators subset or use the /committee endpoint to query all the corresponding duties"

// ValidatorTracesCore contains the core logic for ValidatorTraces without any HTTP concerns.
func (e *Exporter) ValidatorTracesCore(request *ValidatorTracesQuery) (*ValidatorTracesResult, *multierror.Error) {
	if err := e.validateValidatorRequest(request); err != nil {
		return nil, multierror.Append(nil, &ValidationError{Err: err})
	}

	var results []ValidatorCommitteeTrace
	var errs *multierror.Error

	indices, indicesErr := e.extractIndices(request)
	errs = multierror.Append(errs, indicesErr)

	// if the request was for a specific set of participants and we couldn't resolve any, we're done
	if request.HasFilters() && len(indices) == 0 {
		return nil, multierror.Append(nil, &ValidationError{Err: indicesErr})
	}

	// request validation only gates on 'from': a window whose tail crosses
	// Boole reaches post-fork slots without pubkeys/indices. Fork state is
	// monotonic in slot, so that tail is one contiguous range per role —
	// record where it starts and report it as a single non-fatal note per
	// role below, rather than allocating one note per skipped slot.
	postForkNoteFrom := map[spectypes.BeaconRole]phase0.Slot{}

	for s := request.From; s <= request.To; s++ {
		slot := phase0.Slot(s)
		for _, role := range request.Roles {
			isCommittee := e.isCommitteeDutyAtSlot(role, slot)
			if isCommittee && len(indices) == 0 {
				if _, ok := postForkNoteFrom[role]; !ok {
					postForkNoteFrom[role] = slot
				}
				continue
			}

			providerFunc := e.getValidatorDutiesForRoleAndSlot
			if isCommittee {
				providerFunc = e.getValidatorCommitteeDutiesForRoleAndSlot
			}

			duties, err := providerFunc(role, slot, indices)
			results = append(results, duties...)
			errs = multierror.Append(errs, err)
		}
	}

	for _, role := range request.Roles {
		noteFrom, ok := postForkNoteFrom[role]
		if !ok {
			continue
		}
		delete(postForkNoteFrom, role) // guard against duplicate roles in the request
		errs = multierror.Append(errs, fmt.Errorf("%w: slots %d-%d: role %s, %s", ErrPostForkCommitteeDutyNote, noteFrom, request.To, role.String(), committeeDutyFilterHint))
	}

	// by design, not found duties are expected and not considered as API errors
	errs = filterOutDutyNotFoundErrors(errs)

	// Build schedule from disk, read-only.
	schedule := e.buildValidatorSchedule(request, indices)

	return &ValidatorTracesResult{Traces: results, Schedule: schedule}, errs
}

func (e *Exporter) validateValidatorRequest(request *ValidatorTracesQuery) error {
	if err := validateSlotRange(request.From, request.To); err != nil {
		return err
	}

	if len(request.Roles) == 0 {
		return fmt.Errorf("at least one role is required")
	}

	// either PubKeys or Indices are required for committee duty roles.
	// Fork state is evaluated at the range's lower bound so that a window
	// whose tail crosses Boole still serves its pre-fork portion; the
	// post-fork tail is reported as a non-fatal note in the per-slot loop.
	if len(request.PubKeys) == 0 && len(request.Indices) == 0 {
		for _, role := range request.Roles {
			if e.isCommitteeDutyAtSlot(role, phase0.Slot(request.From)) {
				return fmt.Errorf("role %s is a committee duty, %s", role.String(), committeeDutyFilterHint)
			}
		}
	}

	return nil
}

func (e *Exporter) getValidatorDutiesForRoleAndSlot(role spectypes.BeaconRole, slot phase0.Slot, indices []phase0.ValidatorIndex) ([]ValidatorCommitteeTrace, error) {
	if len(indices) == 0 {
		traces, err := e.traceStore.GetValidatorDuties(role, slot)
		out := make([]ValidatorCommitteeTrace, 0, len(traces))
		for _, t := range traces {
			out = append(out, ValidatorCommitteeTrace{ValidatorDutyTrace: *t})
		}
		return out, err
	}

	duties := make([]ValidatorCommitteeTrace, 0, len(indices))
	var errs *multierror.Error

	for _, idx := range indices {
		var result ValidatorCommitteeTrace

		duty, err := e.traceStore.GetValidatorDuty(role, slot, idx)
		if err != nil {
			e.logger.Error("error getting validator duty", zap.Error(err), fields.Slot(slot), fields.ValidatorIndex(idx))
			errs = multierror.Append(errs, err)
			continue
		}
		result.ValidatorDutyTrace = *duty

		// best effort attempt to fill the CommitteeID field, non blocking if it fails
		// as the duty itself is still valid without it
		committeeID, err := e.traceStore.GetCommitteeID(slot, idx)
		if err == nil {
			result.CommitteeID = &committeeID
		} else if !isNotFoundError(err) {
			// if error is not found, nothing to report to prevent unnecessary noise, otherwise log the error:
			e.logger.Debug("error getting committee ID", zap.Error(err), fields.Slot(slot), fields.ValidatorIndex(idx))
			errs = multierror.Append(errs, err)
		}

		duties = append(duties, result)
	}
	return duties, errs.ErrorOrNil()
}

func (e *Exporter) getValidatorCommitteeDutiesForRoleAndSlot(role spectypes.BeaconRole, slot phase0.Slot, indices []phase0.ValidatorIndex) ([]ValidatorCommitteeTrace, error) {
	results := make([]ValidatorCommitteeTrace, 0, len(indices))
	var errs *multierror.Error

	// This helper is only reached for committee-backed beacon roles (see isCommitteeDutyAtSlot),
	// which resolve to either the RoleCommittee or RoleAggregatorCommittee runner role.
	runnerRole, ok := ssvtypes.CommitteeRunnerRoleForBeaconRole(role)
	if !ok {
		return nil, fmt.Errorf("unexpected committee-backed beacon role: %s", role.String())
	}
	bucket, ok := ssvtypes.CommitteeSignerBucketForBeaconRole(role)
	if !ok {
		return nil, fmt.Errorf("no signer bucket for committee-backed beacon role: %s", role.String())
	}

	for _, index := range indices {
		committeeID, err := e.traceStore.GetCommitteeID(slot, index)
		if err != nil {
			e.logger.Debug("error getting committee ID", zap.Error(err), fields.Slot(slot), fields.ValidatorIndex(index))
			errs = multierror.Append(errs, err)
			continue
		}

		duty, err := e.traceStore.GetCommitteeDuty(slot, committeeID, runnerRole)
		if err != nil {
			e.logger.Error("error getting committee duty", zap.Error(err), fields.Slot(slot), fields.BeaconRole(role), fields.ValidatorIndex(index))
			errs = multierror.Append(errs, err)
			continue
		}

		// Membership gating: only return a validator entry if this index appears
		// in the role-specific signer data collected for this committee duty.
		hasIndex := false
		switch bucket {
		case ssvtypes.CommitteeSignerBucketAttester:
			for _, sd := range duty.Attester {
				if slices.Contains(sd.ValidatorIdx, index) {
					hasIndex = true
					break
				}
			}
		case ssvtypes.CommitteeSignerBucketSyncCommittee:
			for _, sd := range duty.SyncCommittee {
				if slices.Contains(sd.ValidatorIdx, index) {
					hasIndex = true
					break
				}
			}
		default:
			// For non-committee roles, should not reach this path; be conservative.
			hasIndex = true
		}
		if !hasIndex {
			continue
		}

		validatorDuty := ValidatorCommitteeTrace{
			ValidatorDutyTrace: traces.ValidatorDutyTrace{
				ConsensusTrace: duty.ConsensusTrace,
				Slot:           duty.Slot,
				Validator:      index,
				Role:           role,
			},
			CommitteeID: &committeeID,
		}

		results = append(results, validatorDuty)
	}

	return results, errs.ErrorOrNil()
}

// buildValidatorSchedule reads the compact on-disk schedule and returns a filtered
// per-validator schedule for the requested roles and slot range.
func (e *Exporter) buildValidatorSchedule(req *ValidatorTracesQuery, indices []phase0.ValidatorIndex) []ValidatorScheduleEntry {
	out := make([]ValidatorScheduleEntry, 0)

	// Deduplicate requested roles (idiomatic way is to build a map in ~O(n) cost).
	roleWanted := map[spectypes.BeaconRole]struct{}{}
	for _, r := range req.Roles {
		roleWanted[r] = struct{}{}
	}

	// If no filters provided, we’ll include all indices present in the schedule per slot.
	filter := req.HasFilters()

	for s := req.From; s <= req.To; s++ {
		slot := phase0.Slot(s)
		sched, err := e.traceStore.GetScheduled(slot)
		if err != nil {
			e.logger.Warn("get scheduled failed", zap.Error(err), fields.Slot(slot))
			continue
		}

		// Determine which indices to include.
		var idxs []phase0.ValidatorIndex
		if filter {
			idxs = indices
		} else {
			idxs = make([]phase0.ValidatorIndex, 0, len(sched))
			for idx := range sched {
				idxs = append(idxs, idx)
			}
		}

		for _, idx := range idxs {
			mask, ok := sched[idx]
			if !ok {
				continue
			}
			roles := make([]spectypes.BeaconRole, 0, len(roleWanted))
			for role := range roleWanted {
				if rolemask.Has(mask, role) {
					roles = append(roles, role)
				}
			}
			if len(roles) == 0 {
				// If the request specified explicit indices/pubkeys, include the
				// validator entry with empty roles to make absence explicit.
				if filter {
					out = append(out, ValidatorScheduleEntry{Slot: slot, Validator: idx, Roles: roles})
				}
				continue
			}
			out = append(out, ValidatorScheduleEntry{
				Slot:      slot,
				Validator: idx,
				Roles:     roles,
			})
		}
	}
	return out
}

// === Shared validator traces helpers ===

// isCommitteeDutyAtSlot reports whether the given beacon role resolves to a
// committee-backed duty at the given slot. ATTESTER/SYNC_COMMITTEE are always
// committee duties. AGGREGATOR/SYNC_COMMITTEE_CONTRIBUTION are validator duties
// pre-Boole and committee duties post-Boole.
func (e *Exporter) isCommitteeDutyAtSlot(role spectypes.BeaconRole, slot phase0.Slot) bool {
	runnerRole, ok := ssvtypes.CommitteeRunnerRoleForBeaconRole(role)
	if !ok {
		return false
	}
	if runnerRole == spectypes.RoleAggregatorCommittee {
		// Alan: AGGREGATOR/SYNC_COMMITTEE_CONTRIBUTION are validator duties.
		// Boole: AGGREGATOR/SYNC_COMMITTEE_CONTRIBUTION are committee duties.
		return e.networkConfig.BooleForkAtSlot(slot)
	}
	return true
}
