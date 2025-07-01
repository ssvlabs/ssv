package handlers

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/api"
	model "github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/exporter/store"
	ibftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/logging/fields"
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
	GetValidatorDuty(role spectypes.BeaconRole, slot phase0.Slot, pubkey spectypes.ValidatorPK) (*dutytracer.ValidatorDutyTrace, error)
	GetAllValidatorDuties(role spectypes.BeaconRole, slot phase0.Slot) ([]*dutytracer.ValidatorDutyTrace, error)
	GetCommitteeDuty(slot phase0.Slot, committeeID spectypes.CommitteeID, role ...spectypes.BeaconRole) (*model.CommitteeDutyTrace, error)
	GetCommitteeDuties(slot phase0.Slot, roles ...spectypes.BeaconRole) ([]*model.CommitteeDutyTrace, error)
	GetCommitteeID(slot phase0.Slot, pubkey spectypes.ValidatorPK) (spectypes.CommitteeID, phase0.ValidatorIndex, error)
	GetCommitteeDutyLinks(slot phase0.Slot) ([]*model.CommitteeDutyLink, error)
	GetValidatorDecideds(role spectypes.BeaconRole, slot phase0.Slot, pubKeys []spectypes.ValidatorPK) ([]qbftstorage.ParticipantsRangeEntry, error)
	GetAllValidatorDecideds(role spectypes.BeaconRole, slot phase0.Slot) ([]qbftstorage.ParticipantsRangeEntry, error)
	GetCommitteeDecideds(slot phase0.Slot, pubKey spectypes.ValidatorPK, roles ...spectypes.BeaconRole) ([]qbftstorage.ParticipantsRangeEntry, error)
	GetAllCommitteeDecideds(slot phase0.Slot, roles ...spectypes.BeaconRole) ([]qbftstorage.ParticipantsRangeEntry, error)
}

type ParticipantResponse struct {
	Role      string `json:"role"`
	Slot      uint64 `json:"slot"`
	PublicKey string `json:"public_key"`
	Message   struct {
		// We're keeping "Signers" capitalized to avoid breaking existing clients that rely on the current structure
		Signers []uint64 `json:"Signers"`
	} `json:"message"`
}

func (e *Exporter) Decideds(w http.ResponseWriter, r *http.Request) error {
	var request struct {
		From    uint64        `json:"from"`
		To      uint64        `json:"to"`
		Roles   api.RoleSlice `json:"roles"`
		PubKeys api.HexSlice  `json:"pubkeys"`
	}
	var response struct {
		Data []*ParticipantResponse `json:"data"`
	}

	if err := api.Bind(r, &request); err != nil {
		return api.BadRequestError(err)
	}

	if request.From > request.To {
		return api.BadRequestError(fmt.Errorf("'from' must be less than or equal to 'to'"))
	}

	if len(request.Roles) == 0 {
		return api.BadRequestError(fmt.Errorf("at least one role is required"))
	}

	response.Data = []*ParticipantResponse{}
	from := phase0.Slot(request.From)
	to := phase0.Slot(request.To)

	var pubkeys []spectypes.ValidatorPK
	for i, req := range request.PubKeys {
		var pubkey spectypes.ValidatorPK
		if len(req) != len(pubkey) {
			return api.BadRequestError(fmt.Errorf("invalid pubkey length at index %d", i))
		}
		copy(pubkey[:], req)
		pubkeys = append(pubkeys, pubkey)
	}

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

		for _, pubKey := range pubkeys {
			participantsByPK, err := store.GetParticipantsInRange(pubKey, from, to)
			if err != nil {
				return api.Error(fmt.Errorf("error getting participants: %w", err))
			}
			participantsRange = append(participantsRange, participantsByPK...)
		}

		// map to API response
		for _, pr := range participantsRange {
			response.Data = append(response.Data, transformToParticipantResponse(role, pr))
		}
	}

	return api.Render(w, r, response)
}

func (e *Exporter) TraceDecideds(w http.ResponseWriter, r *http.Request) error {
	var request struct {
		From    uint64        `json:"from"`
		To      uint64        `json:"to"`
		Roles   api.RoleSlice `json:"roles"`
		PubKeys api.HexSlice  `json:"pubkeys"`
	}
	var response struct {
		Data []*ParticipantResponse `json:"data"`
	}

	if err := api.Bind(r, &request); err != nil {
		return api.BadRequestError(err)
	}

	if request.From > request.To {
		return api.BadRequestError(fmt.Errorf("'from' must be less than or equal to 'to'"))
	}

	if len(request.Roles) == 0 {
		return api.BadRequestError(fmt.Errorf("at least one role is required"))
	}

	// Initialize with empty slice to ensure we always return [] instead of null
	response.Data = make([]*ParticipantResponse, 0)

	var pubkeys []spectypes.ValidatorPK
	for _, req := range request.PubKeys {
		var pubkey spectypes.ValidatorPK
		if len(req) != len(pubkey) {
			return api.BadRequestError(fmt.Errorf("invalid pubkey length: %s", req))
		}
		copy(pubkey[:], req)
		pubkeys = append(pubkeys, pubkey)
	}

	for _, r := range request.Roles {
		role := spectypes.BeaconRole(r)
		switch role {
		case spectypes.BNRoleAttester, spectypes.BNRoleSyncCommittee:
			for s := request.From; s <= request.To; s++ {
				slot := phase0.Slot(s)

				// if no pubkeys are provided, get all decideds for the role
				if len(pubkeys) == 0 {
					participantsByPK, err := e.traceStore.GetAllCommitteeDecideds(slot, role)
					if err != nil {
						return api.Error(fmt.Errorf("error getting all committee decideds for slot %d and role %s: %w", slot, role.String(), err))
					}
					for _, pr := range participantsByPK {
						// duty syncer fails to parse messages with no signers so instead
						// we skip adding the message to the response altogether
						if len(pr.Signers) == 0 {
							continue
						}
						response.Data = append(response.Data, transformToParticipantResponse(role, pr))
					}
				}

				// otherwise iterate over the provided pubkeys
				for _, pubkey := range pubkeys {
					participantsByPK, err := e.traceStore.GetCommitteeDecideds(slot, pubkey, role)
					if err != nil {
						if errors.Is(err, dutytracer.ErrNotFound) || errors.Is(err, store.ErrNotFound) {
							e.logger.Debug("error getting committee decideds", zap.Error(err), fields.Slot(slot), fields.BeaconRole(role), fields.Validator(pubkey[:]))
							// we might not have a duty for this role, so we skip it
							continue
						}
						return api.Error(fmt.Errorf("error getting committee duty for slot %d and pubkey %s and role %s: %w", slot, hex.EncodeToString(pubkey[:]), role.String(), err))
					}
					for _, pr := range participantsByPK {
						// duty syncer fails to parse messages with no signers so instead
						// we skip adding the message to the response altogether
						if len(pr.Signers) == 0 {
							continue
						}
						response.Data = append(response.Data, transformToParticipantResponse(role, pr))
					}
				}
			}
		default:
			if len(pubkeys) == 0 {
				for s := request.From; s <= request.To; s++ {
					slot := phase0.Slot(s)
					participantsByPK, err := e.traceStore.GetAllValidatorDecideds(role, slot)
					if err != nil {
						e.logger.Debug("error getting all validator decideds", zap.Error(err), fields.Slot(slot), fields.BeaconRole(role))
						continue
					}
					for _, pr := range participantsByPK {
						// duty syncer fails to parse messages with no signers so instead
						// we skip adding the message to the response altogether
						if len(pr.Signers) == 0 {
							continue
						}
						response.Data = append(response.Data, transformToParticipantResponse(role, pr))
					}
				}

				continue
			}

			for s := request.From; s <= request.To; s++ {
				slot := phase0.Slot(s)
				participantsByPK, err := e.traceStore.GetValidatorDecideds(role, slot, pubkeys)
				if err != nil {
					e.logger.Debug("error getting validator decideds", zap.Error(err), fields.Slot(slot), fields.BeaconRole(role))
					continue
				}
				for _, pr := range participantsByPK {
					// duty syncer fails to parse messages with no signers so instead
					// we skip adding the message to the response altogether
					if len(pr.Signers) == 0 {
						continue
					}
					response.Data = append(response.Data, transformToParticipantResponse(role, pr))
				}
			}
		}
	}

	return api.Render(w, r, response)
}

func transformToParticipantResponse(role spectypes.BeaconRole, entry qbftstorage.ParticipantsRangeEntry) *ParticipantResponse {
	response := &ParticipantResponse{
		Role:      role.String(),
		Slot:      uint64(entry.Slot),
		PublicKey: hex.EncodeToString(entry.PubKey[:]),
	}
	response.Message.Signers = entry.Signers

	return response
}

func (e *Exporter) CommitteeTraces(w http.ResponseWriter, r *http.Request) error {
	var request struct {
		From         uint64       `json:"from"`
		To           uint64       `json:"to"`
		CommitteeIDs api.HexSlice `json:"committeeIDs"`
	}

	if err := api.Bind(r, &request); err != nil {
		return api.BadRequestError(err)
	}

	if request.From > request.To {
		return api.BadRequestError(fmt.Errorf("'from' must be less than or equal to 'to'"))
	}

	if len(request.CommitteeIDs) == 0 {
		var all []*model.CommitteeDutyTrace
		for s := request.From; s <= request.To; s++ {
			slot := phase0.Slot(s)
			duties, err := e.traceStore.GetCommitteeDuties(slot)
			if err != nil {
				e.logger.Debug("error getting all committee duties", zap.Error(err), fields.Slot(slot))
				continue
			}
			all = append(all, duties...)
		}
		return api.Render(w, r, toCommitteeTraceResponse(all))
	}

	var committeeIDs []spectypes.CommitteeID

	for _, cmt := range request.CommitteeIDs {
		var id spectypes.CommitteeID
		if len(cmt) != len(id) {
			return api.BadRequestError(fmt.Errorf("invalid committee ID length: %s", hex.EncodeToString(cmt)))
		}
		copy(id[:], cmt)
		committeeIDs = append(committeeIDs, id)
	}

	var duties []*model.CommitteeDutyTrace
	for _, cmtID := range committeeIDs {
		for s := request.From; s <= request.To; s++ {
			slot := phase0.Slot(s)
			duty, err := e.traceStore.GetCommitteeDuty(slot, cmtID)
			if err != nil {
				e.logger.Debug("error getting committee duty", zap.Error(err), fields.Slot(slot), fields.CommitteeID(cmtID))
				continue
			}
			duties = append(duties, duty)
		}
	}

	return api.Render(w, r, toCommitteeTraceResponse(duties))
}

func toCommitteeTraceResponse(duties []*model.CommitteeDutyTrace) *committeeTraceResponse {
	r := new(committeeTraceResponse)
	r.Data = make([]committeeTrace, 0)
	for _, t := range duties {
		r.Data = append(r.Data, toCommitteeTrace(t))
	}
	return r
}

type validatorTracesRequest struct {
	From    uint64          `json:"from"`
	To      uint64          `json:"to"`
	Roles   api.RoleSlice   `json:"roles"`
	PubKeys api.HexSlice    `json:"pubkeys"`
	Indices api.Uint64Slice `json:"indices"`
}

func (e *Exporter) ValidatorTraces(w http.ResponseWriter, r *http.Request) error {
	var request validatorTracesRequest

	if err := api.Bind(r, &request); err != nil {
		return api.BadRequestError(err)
	}

	if request.From > request.To {
		return api.BadRequestError(fmt.Errorf("'from' must be less than or equal to 'to'"))
	}

	if len(request.Roles) == 0 {
		return api.BadRequestError(fmt.Errorf("at least one role is required"))
	}

	if len(request.PubKeys) == 0 && len(request.Indices) == 0 {
		return e.getAllValidatorTraces(w, r, request)
	}

	var pubkeys []spectypes.ValidatorPK

	for _, index := range request.Indices {
		share, found := e.validators.ValidatorByIndex(phase0.ValidatorIndex(index))
		if !found {
			return api.BadRequestError(fmt.Errorf("validator not found: %d", index))
		}
		pubkeys = append(pubkeys, share.ValidatorPubKey)
	}

	for _, req := range request.PubKeys {
		var pubkey spectypes.ValidatorPK
		if len(req) != len(pubkey) {
			return api.BadRequestError(fmt.Errorf("invalid pubkey length: %s", req))
		}
		copy(pubkey[:], req)
		pubkeys = append(pubkeys, pubkey)
	}

	slices.SortFunc(pubkeys, func(a, b spectypes.ValidatorPK) int {
		return bytes.Compare(a[:], b[:])
	})
	pubkeys = slices.Compact(pubkeys)

	var results []*dutytracer.ValidatorDutyTrace

	for s := request.From; s <= request.To; s++ {
		slot := phase0.Slot(s)
		for _, r := range request.Roles {
			role := spectypes.BeaconRole(r)
			for _, pubkey := range pubkeys {
				if role == spectypes.BNRoleSyncCommittee || role == spectypes.BNRoleAttester {
					committeeID, index, err := e.traceStore.GetCommitteeID(slot, pubkey)
					if err != nil {
						e.logger.Debug("error getting committee ID", zap.Error(err), fields.Slot(slot), fields.Validator(pubkey[:]))
						continue
					}
					duty, err := e.traceStore.GetCommitteeDuty(slot, committeeID, role)
					if err != nil {
						if errors.Is(err, dutytracer.ErrNotFound) || errors.Is(err, store.ErrNotFound) {
							e.logger.Debug("error getting committee duty", zap.Error(err), fields.Slot(slot), fields.BeaconRole(role), fields.Validator(pubkey[:]))
							// we might not have a duty for this role, so we skip it
							continue
						}
						return api.Error(fmt.Errorf("error getting committee duty: %w", err))
					}

					validatorDuty := &dutytracer.ValidatorDutyTrace{
						CommitteeID: committeeID,
						ValidatorDutyTrace: model.ValidatorDutyTrace{
							ConsensusTrace: duty.ConsensusTrace,
							Slot:           duty.Slot,
							Validator:      index,
							Role:           role,
						},
					}

					results = append(results, validatorDuty)

					continue
				}

				duty, err := e.traceStore.GetValidatorDuty(role, slot, pubkey)
				if err != nil {
					e.logger.Debug("error getting validator duty", zap.Error(err), fields.Slot(slot), fields.BeaconRole(role), fields.Validator(pubkey[:]))
					continue
				}
				results = append(results, duty)
			}
		}
	}

	return api.Render(w, r, toValidatorTraceResponse(results))
}

func (e *Exporter) getAllValidatorTraces(w http.ResponseWriter, r *http.Request, request validatorTracesRequest) error {
	var results []*dutytracer.ValidatorDutyTrace

	for s := request.From; s <= request.To; s++ {
		slot := phase0.Slot(s)
		for _, r := range request.Roles {
			role := spectypes.BeaconRole(r)
			if role == spectypes.BNRoleSyncCommittee || role == spectypes.BNRoleAttester {
				links, err := e.traceStore.GetCommitteeDutyLinks(slot)
				if err != nil {
					e.logger.Debug("error getting all committee links", zap.Error(err), fields.Slot(slot), fields.BeaconRole(role))
					continue
				}
				for _, link := range links {
					duty, err := e.traceStore.GetCommitteeDuty(slot, link.CommitteeID, role)
					if err != nil {
						e.logger.Debug("error getting committee duty", zap.Error(err), fields.Slot(slot), fields.CommitteeID(link.CommitteeID), fields.BeaconRole(role))
						continue
					}
					validatorDuty := &dutytracer.ValidatorDutyTrace{
						CommitteeID: link.CommitteeID,
						ValidatorDutyTrace: model.ValidatorDutyTrace{
							ConsensusTrace: duty.ConsensusTrace,
							Slot:           duty.Slot,
							Validator:      link.ValidatorIndex,
							Role:           role,
						},
					}

					results = append(results, validatorDuty)

					continue
				}
			} else {
				duties, err := e.traceStore.GetAllValidatorDuties(role, slot)
				if err != nil {
					e.logger.Debug("error getting validator duty", zap.Error(err), fields.Slot(slot), fields.BeaconRole(role))
					continue
				}

				results = append(results, duties...)
			}
		}
	}

	return api.Render(w, r, toValidatorTraceResponse(results))
}

func toValidatorTraceResponse(duties []*dutytracer.ValidatorDutyTrace) *validatorTraceResponse {
	var zeroCommitteeID spectypes.CommitteeID
	r := new(validatorTraceResponse)
	r.Data = make([]validatorTrace, 0)
	for _, t := range duties {
		trace := toValidatorTrace(&t.ValidatorDutyTrace)
		if t.CommitteeID != zeroCommitteeID {
			trace.CommitteeID = hex.EncodeToString(t.CommitteeID[:])
		}
		r.Data = append(r.Data, trace)
	}
	return r
}
