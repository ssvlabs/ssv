package exporter2

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/hashicorp/go-multierror"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ibft/storage"
	dutytracer "github.com/ssvlabs/ssv/operator/dutytracer"
)

// TraceDecidedsCore contains the core logic for TraceDecideds without any HTTP concerns.
func (e *Exporter) TraceDecidedsCore(request *DecidedsQuery) (*TraceDecidedsResult, *multierror.Error) {
	if err := validateDecidedRequest(request); err != nil {
		return nil, multierror.Append(nil, &ValidationError{Err: err})
	}

	var participants = make([]DecidedParticipant, 0)
	var errs *multierror.Error

	indices, indicesErr := e.indicesFromDecidedsQuery(request)
	errs = multierror.Append(errs, indicesErr)

	// if the request was for a specific set of participants and we couldn't resolve any, we're done
	if request.HasFilters() && len(indices) == 0 {
		// for retro-compatibility we should return a validation error here
		return nil, multierror.Append(nil, &ValidationError{Err: indicesErr})
	}

	for _, role := range request.Roles {
		for s := request.From; s <= request.To; s++ {
			slot := phase0.Slot(s)

			var roleParticipantsIdx []dutytracer.ParticipantsRangeIndexEntry
			var roleErrs *multierror.Error

			switch role {
			case spectypes.BNRoleAttester, spectypes.BNRoleSyncCommittee:
				roleParticipantsIdx, roleErrs = e.getCommitteeDecidedsForRole(slot, indices, role)
			default:
				roleParticipantsIdx, roleErrs = e.getValidatorDecidedsForRole(slot, indices, role)
			}

			errs = multierror.Append(errs, roleErrs)

			for _, idxEntry := range roleParticipantsIdx {
				// duty syncer fails to parse messages with no signers so instead
				// we skip adding the message to the response altogether
				if len(idxEntry.Signers) == 0 {
					// we don't append an error here to prevent duty-syncer from getting stuck on error-500.
					// still we should investigate how it is possible that we have an entry with no signers,
					// we log it here for further investigation.
					e.logger.Error("omitting entry with no signers", zap.String("index", fmt.Sprintf("%x", idxEntry.Index)), zap.Uint64("slot", uint64(slot)), zap.String("role", role.String()))
					continue
				}

				// enrich response with validator pubkeys
				pr, convErr := e.toParticipantsRangeEntry(idxEntry)
				if convErr != nil {
					errs = multierror.Append(errs, convErr)
					continue
				}

				participants = append(participants, DecidedParticipantFromRange(role, pr, idxEntry.Index))
			}
		}
	}

	// by design, not found duties are expected and not considered as API errors
	errs = filterOutDutyNotFoundErrors(errs)

	return &TraceDecidedsResult{Participants: participants}, errs
}

// DecidedsCore contains the core logic for the backward-compatible exporter-v1 "decideds" endpoint.
func (e *Exporter) DecidedsCore(request *DecidedsQuery) (*TraceDecidedsResult, error) {
	if err := validateDecidedRequest(request); err != nil {
		return nil, &ValidationError{Err: err}
	}

	// Initialize with empty slice to ensure we always return [] instead of null
	var response TraceDecidedsResult
	response.Participants = make([]DecidedParticipant, 0)

	from := phase0.Slot(request.From)
	to := phase0.Slot(request.To)

	for _, role := range request.Roles {
		store := e.participantStores.Get(role)

		var participantsRange []storage.ParticipantsRangeEntry

		if len(request.PubKeys) == 0 {
			var err error
			participantsRange, err = store.GetAllParticipantsInRange(from, to)
			if err != nil {
				return nil, fmt.Errorf("error getting participants: %w", err)
			}
		}

		for _, pubkey := range request.PubKeys {
			participantsByPK, err := store.GetParticipantsInRange(pubkey, from, to)
			if err != nil {
				return nil, fmt.Errorf("error getting participants: %w", err)
			}
			participantsRange = append(participantsRange, participantsByPK...)
		}

		for _, pr := range participantsRange {
			response.Participants = append(response.Participants, DecidedParticipantFromRange(role, pr, 0))
		}
	}

	return &response, nil
}

func validateDecidedRequest(request *DecidedsQuery) error {
	if request.From > request.To {
		return fmt.Errorf("'from' must be less than or equal to 'to'")
	}

	if len(request.Roles) == 0 {
		return fmt.Errorf("at least one role is required")
	}

	return nil
}

func (e *Exporter) getCommitteeDecidedsForRole(slot phase0.Slot, indices []phase0.ValidatorIndex, role spectypes.BeaconRole) ([]dutytracer.ParticipantsRangeIndexEntry, *multierror.Error) {
	var errs *multierror.Error

	if len(indices) == 0 {
		entries, err := e.traceStore.GetAllCommitteeDecideds(slot, role)
		errs = multierror.Append(errs, err)
		return entries, errs
	}

	var out []dutytracer.ParticipantsRangeIndexEntry
	for _, index := range indices {
		entries, err := e.traceStore.GetCommitteeDecideds(slot, index, role)
		if err != nil {
			errs = multierror.Append(errs, err)
			continue
		}
		out = append(out, entries...)
	}
	return out, errs
}

func (e *Exporter) getValidatorDecidedsForRole(slot phase0.Slot, indices []phase0.ValidatorIndex, role spectypes.BeaconRole) ([]dutytracer.ParticipantsRangeIndexEntry, *multierror.Error) {
	var errs *multierror.Error

	if len(indices) == 0 {
		entries, err := e.traceStore.GetAllValidatorDecideds(role, slot)
		errs = multierror.Append(errs, err)
		return entries, errs
	}

	entries, err := e.traceStore.GetValidatorDecideds(role, slot, indices)
	if err != nil {
		errs = multierror.Append(errs, err)
	}
	return entries, errs
}

// toParticipantsRangeEntry converts an index-based entry into a ParticipantsRangeEntry
// by resolving the validator's pubkey from the registry store.
func (e *Exporter) toParticipantsRangeEntry(ent dutytracer.ParticipantsRangeIndexEntry) (storage.ParticipantsRangeEntry, error) {
	pk, found := e.validators.ValidatorPubkey(ent.Index)
	if !found {
		return storage.ParticipantsRangeEntry{}, fmt.Errorf("validator not found by index: %d", ent.Index)
	}
	return storage.ParticipantsRangeEntry{
		Slot:    ent.Slot,
		PubKey:  pk,
		Signers: ent.Signers,
	}, nil
}
