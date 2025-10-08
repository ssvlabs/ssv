package exporter

import (
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/hashicorp/go-multierror"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/exporter/rolemask"
	"github.com/ssvlabs/ssv/observability/log/fields"
)

// ValidatorTraces godoc
// @Summary Retrieve validator duty traces
// @Description Returns consensus, decided, and message traces for the requested validator duties.
// @Tags Exporter
// @Accept json
// @Produce json
// @Param request query ValidatorTracesRequest false "Filters as query parameters"
// @Param request body ValidatorTracesRequest false "Filters as JSON body"
// @Success 200 {object} ValidatorTracesResponse
// @Failure 400 {object} api.ErrorResponse
// @Failure 429 {object} api.ErrorResponse "Too Many Requests"
// @Failure 500 {object} api.ErrorResponse
// @Router /v1/exporter/traces/validator [get]
// @Router /v1/exporter/traces/validator [post]
func (e *Exporter) ValidatorTraces(w http.ResponseWriter, r *http.Request) error {
	var request ValidatorTracesRequest

	if err := api.Bind(r, &request); err != nil {
		return api.BadRequestError(err)
	}

	if err := validateValidatorRequest(&request); err != nil {
		return api.BadRequestError(err)
	}

	var results []validatorDutyTraceWithCommitteeID
	var errs *multierror.Error

	indices, err := e.extractIndices(&request)
	errs = multierror.Append(errs, err)

	// if the request was for a specific set of participants and we couldn't resolve any, we're done
	if request.hasFilters() && len(indices) == 0 {
		return toApiError(errs)
	}

	for s := request.From; s <= request.To; s++ {
		slot := phase0.Slot(s)
		for _, r := range request.Roles {
			role := spectypes.BeaconRole(r)

			providerFunc := e.getValidatorDutiesForRoleAndSlot
			if isCommitteeDuty(role) {
				providerFunc = e.getValidatorCommitteeDutiesForRoleAndSlot
			}

			duties, err := providerFunc(role, slot, indices)
			results = append(results, duties...)
			errs = multierror.Append(errs, err)
		}
	}

	// by design, not found duties are expected and not considered as API errors
	errs = filterOutDutyNotFoundErrors(errs)

	// if we don't have a single valid result and we have at least one meaningful error, return an error
	if len(results) == 0 && errs.ErrorOrNil() != nil {
		e.logger.Error("error serving SSV API request", zap.Any("request", request), zap.Error(errs))
		return toApiError(errs)
	}

	// Build schedule from disk, read-only.
	schedule := e.buildValidatorSchedule(&request, indices)

	resp := toValidatorTraceResponse(results, errs)
	resp.Schedule = schedule
	return api.Render(w, r, resp)
}

func validateValidatorRequest(request *ValidatorTracesRequest) error {
	if request.From > request.To {
		return fmt.Errorf("'from' must be less than or equal to 'to'")
	}

	if len(request.Roles) == 0 {
		return fmt.Errorf("at least one role is required")
	}

	// either PubKeys or Indices are required for committee duty roles
	if len(request.PubKeys) == 0 && len(request.Indices) == 0 {
		for _, r := range request.Roles {
			role := spectypes.BeaconRole(r)
			if isCommitteeDuty(role) {
				return fmt.Errorf("role %s is a committee duty, please provide either pubkeys or indices to filter the duty for a specific validators subset or use the /committee endpoint to query all the corresponding duties", role.String())
			}
		}
	}

	requiredLength := len(spectypes.ValidatorPK{})
	for _, req := range request.PubKeys {
		if len(req) != requiredLength {
			return fmt.Errorf("invalid pubkey length: %s", hex.EncodeToString(req))
		}
	}

	return nil
}

func (e *Exporter) getValidatorDutiesForRoleAndSlot(role spectypes.BeaconRole, slot phase0.Slot, indices []phase0.ValidatorIndex) ([]validatorDutyTraceWithCommitteeID, error) {
	if len(indices) == 0 {
		traces, err := e.traceStore.GetValidatorDuties(role, slot)
		out := make([]validatorDutyTraceWithCommitteeID, 0, len(traces))
		for _, t := range traces {
			out = append(out, validatorDutyTraceWithCommitteeID{ValidatorDutyTrace: *t})
		}
		return out, err
	}

	duties := make([]validatorDutyTraceWithCommitteeID, 0, len(indices))
	var errs *multierror.Error

	for _, idx := range indices {
		var result validatorDutyTraceWithCommitteeID

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

func (e *Exporter) getValidatorCommitteeDutiesForRoleAndSlot(role spectypes.BeaconRole, slot phase0.Slot, indices []phase0.ValidatorIndex) ([]validatorDutyTraceWithCommitteeID, error) {
	results := make([]validatorDutyTraceWithCommitteeID, 0, len(indices))
	var errs *multierror.Error

	for _, index := range indices {
		committeeID, err := e.traceStore.GetCommitteeID(slot, index)
		if err != nil {
			e.logger.Debug("error getting committee ID", zap.Error(err), fields.Slot(slot), fields.ValidatorIndex(index))
			errs = multierror.Append(errs, err)
			continue
		}

		duty, err := e.traceStore.GetCommitteeDuty(slot, committeeID, role)
		if err != nil {
			e.logger.Error("error getting committee duty", zap.Error(err), fields.Slot(slot), fields.BeaconRole(role), fields.ValidatorIndex(index))
			errs = multierror.Append(errs, err)
			continue
		}

		validatorDuty := validatorDutyTraceWithCommitteeID{
			ValidatorDutyTrace: exporter.ValidatorDutyTrace{
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

func toValidatorTraceResponse(duties []validatorDutyTraceWithCommitteeID, errs *multierror.Error) *ValidatorTracesResponse {
	r := new(ValidatorTracesResponse)
	r.Data = make([]ValidatorTrace, 0)
	for _, t := range duties {
		trace := toValidatorTrace(&t.ValidatorDutyTrace)
		if t.CommitteeID != nil {
			trace.CommitteeID = hex.EncodeToString(t.CommitteeID[:])
		}
		r.Data = append(r.Data, trace)
	}
	r.Errors = toStrings(errs)
	return r
}

// buildValidatorSchedule reads the compact on-disk schedule and returns a filtered
// per-validator schedule for the requested roles and slot range.
func (e *Exporter) buildValidatorSchedule(req *ValidatorTracesRequest, indices []phase0.ValidatorIndex) []ValidatorSchedule {
	out := make([]ValidatorSchedule, 0)
	// Build a quick lookup for requested roles.
	roleWanted := map[spectypes.BeaconRole]struct{}{}
	for _, r := range req.Roles {
		roleWanted[spectypes.BeaconRole(r)] = struct{}{}
	}

	// If no filters provided, we’ll include all indices present in the schedule per slot.
	filter := req.hasFilters()

	for s := req.From; s <= req.To; s++ {
		slot := phase0.Slot(s)
		sched, err := e.traceStore.GetScheduled(slot)
		if err != nil {
			e.logger.Debug("get scheduled failed", zap.Error(err), fields.Slot(slot))
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
			roles := make([]string, 0, len(req.Roles))
			for role := range roleWanted {
				if rolemask.Has(mask, role) {
					roles = append(roles, role.String())
				}
			}
			if len(roles) == 0 {
				// If the request specified explicit indices/pubkeys, include the
				// validator entry with empty roles to make absence explicit.
				if filter {
					out = append(out, ValidatorSchedule{Slot: uint64(slot), Validator: uint64(idx), Roles: roles})
				}
				continue
			}
			out = append(out, ValidatorSchedule{
				Slot:      uint64(slot),
				Validator: uint64(idx),
				Roles:     roles,
			})
		}
	}
	return out
}

// === Shared validator traces helpers ===
func isCommitteeDuty(role spectypes.BeaconRole) bool {
	return role == spectypes.BNRoleSyncCommittee || role == spectypes.BNRoleAttester
}
