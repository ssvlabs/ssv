package handlers

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/hashicorp/go-multierror"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/exporter/store"
	ibftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/observability/log/fields"
	dutytracer "github.com/ssvlabs/ssv/operator/dutytracer"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

type Exporter struct {
	participantStores *ibftstorage.ParticipantStores
	traceStore        dutyTraceStore
	validators        registrystorage.ValidatorStore
	logger            *zap.Logger
}

func NewExporter(logger *zap.Logger, participantStores *ibftstorage.ParticipantStores, traceStore dutyTraceStore, validators registrystorage.ValidatorStore) *Exporter {
	return &Exporter{
		participantStores: participantStores,
		traceStore:        traceStore,
		validators:        validators,
		logger:            logger,
	}
}

type dutyTraceStore interface {
	GetValidatorDuty(role spectypes.BeaconRole, slot phase0.Slot, index phase0.ValidatorIndex) (*exporter.ValidatorDutyTrace, error)
	GetValidatorDuties(role spectypes.BeaconRole, slot phase0.Slot) ([]*exporter.ValidatorDutyTrace, error)
	GetCommitteeDuty(slot phase0.Slot, committeeID spectypes.CommitteeID, role ...spectypes.BeaconRole) (*exporter.CommitteeDutyTrace, error)
	GetCommitteeDuties(slot phase0.Slot, roles ...spectypes.BeaconRole) ([]*exporter.CommitteeDutyTrace, error)
	GetCommitteeID(slot phase0.Slot, index phase0.ValidatorIndex) (spectypes.CommitteeID, error)
	GetValidatorDecideds(role spectypes.BeaconRole, slot phase0.Slot, indices []phase0.ValidatorIndex) ([]dutytracer.ParticipantsRangeIndexEntry, error)
	GetAllValidatorDecideds(role spectypes.BeaconRole, slot phase0.Slot) ([]dutytracer.ParticipantsRangeIndexEntry, error)
	GetCommitteeDecideds(slot phase0.Slot, index phase0.ValidatorIndex, roles ...spectypes.BeaconRole) ([]dutytracer.ParticipantsRangeIndexEntry, error)
	GetAllCommitteeDecideds(slot phase0.Slot, roles ...spectypes.BeaconRole) ([]dutytracer.ParticipantsRangeIndexEntry, error)
}

// === Decideds ======================================================================================

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

func (e *Exporter) Decideds(w http.ResponseWriter, r *http.Request) error {
	var request decidedRequest

	if err := api.Bind(r, &request); err != nil {
		return api.BadRequestError(err)
	}

	if err := validateDecidedRequest(&request); err != nil {
		return api.BadRequestError(err)
	}

	pubkeys := request.parsePubkeys()

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

func (e *Exporter) TraceDecideds(w http.ResponseWriter, r *http.Request) error {
	var request decidedRequest

	if err := api.Bind(r, &request); err != nil {
		return api.BadRequestError(err)
	}

	if err := validateDecidedRequest(&request); err != nil {
		return api.BadRequestError(err)
	}

	pubkeys := request.parsePubkeys()
	var participants = make([]*participantResponse, 0)
	var errs *multierror.Error

	// Collect indices from both pubkeys and indices in the request
	indices := make([]phase0.ValidatorIndex, 0, len(request.Indices)+len(pubkeys))
	for _, idx := range request.Indices {
		indices = append(indices, phase0.ValidatorIndex(idx))
	}
	extra, merr := e.pubkeysToIndices(pubkeys)
	errs = multierror.Append(errs, merr)
	indices = append(indices, extra...)
	slices.Sort(indices)
	indices = slices.Compact(indices)

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

	// if we don't have a single valid participant, return an error
	if len(participants) == 0 && errs.ErrorOrNil() != nil {
		return toApiError(errs)
	}

	// otherwise return a partial response with valid participants
	var response decidedResponse
	response.Data = participants
	response.Errors = toStrings(errs.Errors)
	return api.Render(w, r, response)
}

func toParticipantResponse(role spectypes.BeaconRole, entry qbftstorage.ParticipantsRangeEntry) *participantResponse {
	response := &participantResponse{
		Role:      role.String(),
		Slot:      uint64(entry.Slot),
		PublicKey: hex.EncodeToString(entry.PubKey[:]),
	}
	response.Message.Signers = entry.Signers

	return response
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

func toStrings(errs []error) []string {
	result := make([]string, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			result = append(result, err.Error())
		}
	}
	return result
}

// === CommitteeTraces ======================================================================================

func validateCommitteeRequest(request *committeeRequest) error {
	if request.From > request.To {
		return fmt.Errorf("'from' must be less than or equal to 'to'")
	}

	requiredLength := len(spectypes.CommitteeID{})
	for _, cmt := range request.CommitteeIDs {
		if len(cmt) != requiredLength {
			return fmt.Errorf("invalid committee ID length: %s", hex.EncodeToString(cmt))
		}
	}

	return nil
}

func isNotFoundError(e error) bool {
	return errors.Is(e, store.ErrNotFound) || errors.Is(e, dutytracer.ErrNotFound)
}

func (e *Exporter) getCommitteeDutiesForSlot(slot phase0.Slot, committeeIDs []spectypes.CommitteeID) ([]*exporter.CommitteeDutyTrace, error) {
	if len(committeeIDs) == 0 {
		duties, err := e.traceStore.GetCommitteeDuties(slot)
		return duties, err
	}

	duties := make([]*exporter.CommitteeDutyTrace, 0, len(committeeIDs))

	var errs *multierror.Error
	for _, cmtID := range committeeIDs {
		duty, err := e.traceStore.GetCommitteeDuty(slot, cmtID)
		if err != nil {
			// if error is not found, nothing to report as we might not have a duty for this role
			// otherwise report it:
			if !isNotFoundError(err) {
				e.logger.Error("error getting committee duty", zap.Error(err), fields.Slot(slot), fields.CommitteeID(cmtID))
				errs = multierror.Append(errs, err)
			}
			continue
		}
		duties = append(duties, duty)
	}
	return duties, errs.ErrorOrNil()
}

func (e *Exporter) CommitteeTraces(w http.ResponseWriter, r *http.Request) error {
	var request committeeRequest

	if err := api.Bind(r, &request); err != nil {
		return api.BadRequestError(err)
	}

	if err := validateCommitteeRequest(&request); err != nil {
		return api.BadRequestError(err)
	}

	var all []*exporter.CommitteeDutyTrace
	var errs *multierror.Error
	for s := request.From; s <= request.To; s++ {
		slot := phase0.Slot(s)
		duties, err := e.getCommitteeDutiesForSlot(slot, request.parseCommitteeIds())
		all = append(all, duties...)
		errs = multierror.Append(errs, err)
	}
	return api.Render(w, r, toCommitteeTraceResponse(all, errs))
}

func toCommitteeTraceResponse(duties []*exporter.CommitteeDutyTrace, errs *multierror.Error) *committeeTraceResponse {
	r := new(committeeTraceResponse)
	r.Data = make([]committeeTrace, 0)
	for _, t := range duties {
		r.Data = append(r.Data, toCommitteeTrace(t))
	}
	if errs != nil {
		r.Errors = toStrings(errs.Errors)
	}
	return r
}

// === ValidatorTraces ======================================================================================

func isCommitteeDuty(role spectypes.BeaconRole) bool {
	return role == spectypes.BNRoleSyncCommittee || role == spectypes.BNRoleAttester
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
				return fmt.Errorf("role %s is a committee duty, please provide either pubkeys or indices to filter the duty for specific a validators subset or use the /committee endpoint to query all the corresponding duties", role.String())
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

type validatorDutyTraceWithCommitteeID struct {
	exporter.ValidatorDutyTrace
	CommitteeID *spectypes.CommitteeID
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
			// if error is not found, nothing to report to prevent unnecessary noise
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

func toApiError(errs *multierror.Error) *api.ErrorResponse {
	if len(errs.Errors) == 1 {
		return api.Error(errs.Errors[0])
	}
	return api.Error(errs)
}

// pubkeysToIndices converts pubkeys to indices using the validator store,
// aggregating missing validators into a multierror instead of failing fast.
func (e *Exporter) pubkeysToIndices(pubkeys []spectypes.ValidatorPK) ([]phase0.ValidatorIndex, *multierror.Error) {
	indices := make([]phase0.ValidatorIndex, 0, len(pubkeys))
	var errs *multierror.Error
	for _, pk := range pubkeys {
		idx, ok := e.validators.ValidatorIndex(pk)
		if !ok {
			errs = multierror.Append(errs, fmt.Errorf("validator not found by pubkey: %x", pk))
			continue
		}
		indices = append(indices, idx)
	}
	return indices, errs
}
