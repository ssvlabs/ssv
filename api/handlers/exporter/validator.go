package exporter

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"slices"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/hashicorp/go-multierror"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/observability/log/fields"
)

func (e *Exporter) ValidatorTraces(w http.ResponseWriter, r *http.Request) error {
	var request validatorRequest

	if err := api.Bind(r, &request); err != nil {
		return api.BadRequestError(err)
	}

	if err := validateValidatorRequest(&request); err != nil {
		return api.BadRequestError(err)
	}

	var errs *multierror.Error
	var results []validatorDutyTraceWithCommitteeID

	indices, err := e.extractIndices(&request)
	errs = multierror.Append(errs, err)

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

	if len(results) == 0 && errs.ErrorOrNil() != nil {
		return toApiError(errs)
	}
	return api.Render(w, r, toValidatorTraceResponse(results, errs))
}

func validateValidatorRequest(request *validatorRequest) error {
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
			// if error is not found, nothing to report as we might not have a duty for this role
			// otherwise report it:
			if !isNotFoundError(err) {
				e.logger.Error("error getting validator duty", zap.Error(err), fields.Slot(slot), fields.ValidatorIndex(idx))
				errs = multierror.Append(errs, err)
			}
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
			// if error is not found, nothing to report as we might not have a duty for this role
			// otherwise report it:
			if !isNotFoundError(err) {
				e.logger.Error("error getting committee duty", zap.Error(err), fields.Slot(slot), fields.BeaconRole(role), fields.ValidatorIndex(index))
				errs = multierror.Append(errs, err)
			}
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

func toValidatorTraceResponse(duties []validatorDutyTraceWithCommitteeID, errs *multierror.Error) *validatorTraceResponse {
	r := new(validatorTraceResponse)
	r.Data = make([]validatorTrace, 0)
	for _, t := range duties {
		trace := toValidatorTrace(&t.ValidatorDutyTrace)
		if t.CommitteeID != nil {
			trace.CommitteeID = hex.EncodeToString(t.CommitteeID[:])
		}
		r.Data = append(r.Data, trace)
	}

	if errs.ErrorOrNil() != nil {
		r.Errors = toStrings(errs.Errors)
	}
	return r
}

// === Shared validator traces helpers ===
func isCommitteeDuty(role spectypes.BeaconRole) bool {
	return role == spectypes.BNRoleSyncCommittee || role == spectypes.BNRoleAttester
}

func (e *Exporter) extractIndices(request *validatorRequest) ([]phase0.ValidatorIndex, error) {
	indices := make([]phase0.ValidatorIndex, 0, len(request.Indices)+len(request.PubKeys))
	var errs *multierror.Error

	for _, idx := range request.Indices {
		indices = append(indices, phase0.ValidatorIndex(idx))
	}

	for _, req := range request.PubKeys {
		var pubkey spectypes.ValidatorPK
		copy(pubkey[:], req)
		idx, ok := e.validators.ValidatorIndex(pubkey)
		if !ok {
			errs = multierror.Append(errs, fmt.Errorf("validator not found for pubkey: %x", pubkey))
			continue
		}
		indices = append(indices, idx)
	}

	slices.Sort(indices)
	indices = slices.Compact(indices)

	return indices, errs.ErrorOrNil()
}
