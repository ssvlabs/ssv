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
	dutytracer "github.com/ssvlabs/ssv/operator/dutytracer"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
)

func (e *Exporter) TraceDecideds(w http.ResponseWriter, r *http.Request) error {
	var request decidedRequest

	if err := api.Bind(r, &request); err != nil {
		return api.BadRequestError(err)
	}

	if err := validateDecidedRequest(&request); err != nil {
		return api.BadRequestError(err)
	}

	var participants = make([]*participantResponse, 0)
	var errs *multierror.Error

	indices, err := e.extractIndices(&request)
	errs = multierror.Append(errs, err)

	// if the request was for a specific set of participants and we couldn't resolve any, we're done
	if request.hasFilters() && len(indices) == 0 {
		return toApiError(errs)
	}

	for _, r := range request.Roles {
		role := spectypes.BeaconRole(r)

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
					errs = multierror.Append(errs, fmt.Errorf("omitting entry with no signers (index=%x, slot=%d, role=%s)", idxEntry.Index, slot, role.String()))
					continue
				}

				// enrich response with validator pubkeys
				pr, convErr := e.toParticipantsRangeEntry(idxEntry)
				if convErr != nil {
					errs = multierror.Append(errs, convErr)
					continue
				}

				participants = append(participants, toParticipantResponse(role, pr))
			}
		}
	}

	// by design, not found duties are expected and not considered as API errors
	errs = filterOutDutyNotFoundErrors(errs)

	// if we don't have a single valid result and we have at least one meaningful error, return an error
	if len(participants) == 0 && errs.ErrorOrNil() != nil {
		e.logger.Error("error serving SSV API request", zap.Any("request", request), zap.Error(errs))
		return toApiError(errs)
	}

	// otherwise return a partial response with valid participants
	var response decidedResponse
	response.Data = participants
	response.Errors = toStrings(errs)
	return api.Render(w, r, response)
}

// backward-compatible handler for exporter-v1 "decideds" endpoint
func (e *Exporter) Decideds(w http.ResponseWriter, r *http.Request) error {
	var request decidedRequest

	if err := api.Bind(r, &request); err != nil {
		return api.BadRequestError(err)
	}

	if err := validateDecidedRequest(&request); err != nil {
		return api.BadRequestError(err)
	}

	pubkeys := request.pubKeys()

	// Initialize with empty slice to ensure we always return [] instead of null
	var response decidedResponse
	response.Data = make([]*participantResponse, 0)

	from := phase0.Slot(request.From)
	to := phase0.Slot(request.To)

	for _, r := range request.Roles {
		role := spectypes.BeaconRole(r)
		store := e.participantStores.Get(role)

		var participantsRange []qbftstorage.ParticipantsRangeEntry

		if len(pubkeys) == 0 {
			var err error
			participantsRange, err = store.GetAllParticipantsInRange(from, to)
			if err != nil {
				return api.Error(fmt.Errorf("error getting participants: %w", err))
			}
		}

		for _, pubkey := range pubkeys {
			participantsByPK, err := store.GetParticipantsInRange(pubkey, from, to)
			if err != nil {
				return api.Error(fmt.Errorf("error getting participants: %w", err))
			}
			participantsRange = append(participantsRange, participantsByPK...)
		}

		// map to API response
		for _, pr := range participantsRange {
			response.Data = append(response.Data, toParticipantResponse(role, pr))
		}
	}

	return api.Render(w, r, response)
}

func validateDecidedRequest(request *decidedRequest) error {
	if request.From > request.To {
		return fmt.Errorf("'from' must be less than or equal to 'to'")
	}

	if len(request.Roles) == 0 {
		return fmt.Errorf("at least one role is required")
	}

	requiredLength := len(spectypes.ValidatorPK{})
	for _, req := range request.PubKeys {
		if len(req) != requiredLength {
			return fmt.Errorf("invalid pubkey length: %s", hex.EncodeToString(req))
		}
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
func (e *Exporter) toParticipantsRangeEntry(ent dutytracer.ParticipantsRangeIndexEntry) (qbftstorage.ParticipantsRangeEntry, error) {
	share, found := e.validators.ValidatorByIndex(ent.Index)
	if !found {
		return qbftstorage.ParticipantsRangeEntry{}, fmt.Errorf("validator not found by index: %d", ent.Index)
	}
	return qbftstorage.ParticipantsRangeEntry{
		Slot:    ent.Slot,
		PubKey:  share.ValidatorPubKey,
		Signers: ent.Signers,
	}, nil
}
