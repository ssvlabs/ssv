package handlers

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/api"
	model "github.com/ssvlabs/ssv/exporter/v2"
	ibftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/networkconfig"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

/*
TODO
- migration - populate mapping pubkey -> index for existing db data
	- using the new format we update on every 'saveShare'
*/

type Exporter struct {
	NetworkConfig     networkconfig.NetworkConfig
	ParticipantStores *ibftstorage.ParticipantStores
	TraceStore        DutyTraceStore
	Validators        registrystorage.ValidatorStore
}

type DutyTraceStore interface {
	GetValidatorDuties(role spectypes.BeaconRole, slot phase0.Slot, pubkeys []spectypes.ValidatorPK) ([]*model.ValidatorDutyTrace, error)
	GetCommitteeDutiesByOperator(indices []spectypes.OperatorID, slot phase0.Slot) ([]*model.CommitteeDutyTrace, error)
	GetCommitteeDuty(slot phase0.Slot, committeeID spectypes.CommitteeID) (*model.CommitteeDutyTrace, error)
	GetAllValidatorDuties(role spectypes.BeaconRole, slot phase0.Slot) ([]*model.ValidatorDutyTrace, error)
	GetDecideds(role spectypes.BeaconRole, slot phase0.Slot, pubKeys []spectypes.ValidatorPK) []qbftstorage.ParticipantsRangeEntry
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

	for _, r := range request.Roles {
		role := spectypes.BeaconRole(r)
		store := e.ParticipantStores.Get(role)

		var participantsRange []qbftstorage.ParticipantsRangeEntry

		if len(request.PubKeys) == 0 {
			var err error
			participantsRange, err = store.GetAllParticipantsInRange(from, to)
			if err != nil {
				return api.Error(fmt.Errorf("error getting participants: %w", err))
			}
		}
		// these two^ are mutually exclusive
		for _, pubKey := range request.PubKeys {
			participantsByPK, err := store.GetParticipantsInRange(spectypes.ValidatorPK(pubKey), from, to)
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

	if len(request.PubKeys) == 0 {
		return api.BadRequestError(fmt.Errorf("at least one pubkey is required"))
	}

	response.Data = []*ParticipantResponse{}

	var pubkeys []spectypes.ValidatorPK
	for _, req := range request.PubKeys {
		var pubkey spectypes.ValidatorPK
		if len(req) != len(pubkey) {
			return api.BadRequestError(errors.New("invalid pubkey length"))
		}
		copy(pubkey[:], req)
		pubkeys = append(pubkeys, pubkey)
	}

	for s := request.From; s <= request.To; s++ {
		slot := phase0.Slot(s)
		for _, r := range request.Roles {
			role := spectypes.BeaconRole(r)

			participantsByPK := e.TraceStore.GetDecideds(role, slot, pubkeys)
			for _, pr := range participantsByPK {
				response.Data = append(response.Data, transformToParticipantResponse(role, pr))
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

func (e *Exporter) OperatorTraces(w http.ResponseWriter, r *http.Request) error {
	var request struct {
		From       uint64          `json:"from"`
		To         uint64          `json:"to"`
		OperatorID api.Uint64Slice `json:"operatorID"`
	}

	if err := api.Bind(r, &request); err != nil {
		return api.BadRequestError(err)
	}

	var indexes []spectypes.OperatorID
	indexes = append(indexes, request.OperatorID...)

	var duties []*model.CommitteeDutyTrace
	for s := request.From; s <= request.To; s++ {
		slot := phase0.Slot(s)
		dd, err := e.TraceStore.GetCommitteeDutiesByOperator(indexes, slot)
		if err != nil {
			return api.Error(fmt.Errorf("error getting duties: %w", err))
		}
		duties = append(duties, dd...)
	}

	return api.Render(w, r, toCommitteeTraceResponse(duties))
}

func (e *Exporter) CommitteeTraces(w http.ResponseWriter, r *http.Request) error {
	var request struct {
		From         uint64          `json:"from"`
		To           uint64          `json:"to"`
		CommitteeIDs api.HexSlice    `json:"committeeIDs"`
		Committees   api.Uint64Slice `json:"committees"` // TBD
	}

	if err := api.Bind(r, &request); err != nil {
		return api.BadRequestError(err)
	}

	if request.From > request.To {
		return api.BadRequestError(fmt.Errorf("'from' must be less than or equal to 'to'"))
	}

	if len(request.Committees) == 0 && len(request.CommitteeIDs) == 0 {
		return api.BadRequestError(fmt.Errorf("committees are required"))
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

	if len(committeeIDs) == 0 { // double check
		id := spectypes.GetCommitteeID(request.Committees)
		committeeIDs = append(committeeIDs, id)
	}

	var duties []*model.CommitteeDutyTrace
	for _, cmtID := range committeeIDs {
		for s := request.From; s <= request.To; s++ {
			slot := phase0.Slot(s)
			duty, err := e.TraceStore.GetCommitteeDuty(slot, cmtID)
			if err != nil {
				return api.Error(fmt.Errorf("error getting duties: %w", err))
			}
			duties = append(duties, duty)
		}
	}

	return api.Render(w, r, toCommitteeTraceResponse(duties))
}

func toCommitteeTraceResponse(duties []*model.CommitteeDutyTrace) *committeeTraceResponse {
	r := new(committeeTraceResponse)
	for _, t := range duties {
		r.Data = append(r.Data, toCommitteeTrace(t))
	}
	return r
}

func (e *Exporter) ValidatorTraces(w http.ResponseWriter, r *http.Request) error {
	var request struct {
		From    uint64          `json:"from"`
		To      uint64          `json:"to"`
		Roles   api.RoleSlice   `json:"roles"`
		PubKeys api.HexSlice    `json:"pubkeys"`
		Indices api.Uint64Slice `json:"indices"`
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

	if len(request.PubKeys) == 0 && len(request.Indices) == 0 {
		return api.BadRequestError(errors.New("either pubkeys or indices is required"))
	}

	var pubkeys []spectypes.ValidatorPK

	for _, index := range request.Indices {
		share, f := e.Validators.ValidatorByIndex(phase0.ValidatorIndex(index))
		if !f {
			return api.BadRequestError(fmt.Errorf("validator not found: %d", index))
		}
		pubkeys = append(pubkeys, share.ValidatorPubKey)
	}

	for _, req := range request.PubKeys {
		var pubkey spectypes.ValidatorPK
		if len(req) != len(pubkey) {
			return api.BadRequestError(errors.New("invalid pubkey length"))
		}
		copy(pubkey[:], req)
		pubkeys = append(pubkeys, pubkey)
	}

	var results []*model.ValidatorDutyTrace

	for s := request.From; s <= request.To; s++ {
		slot := phase0.Slot(s)
		for _, r := range request.Roles {
			role := spectypes.BeaconRole(r)
			duties, err := e.TraceStore.GetValidatorDuties(role, slot, pubkeys)
			if err != nil {
				return api.Error(fmt.Errorf("error getting duties: %w", err))
			}
			results = append(results, duties...)
		}
	}

	return api.Render(w, r, toValidatorTraceResponse(results))
}

func toValidatorTraceResponse(duties []*model.ValidatorDutyTrace) *validatorTraceResponse {
	r := new(validatorTraceResponse)
	for _, t := range duties {
		r.Data = append(r.Data, toValidatorTrace(t))
	}
	return r
}
