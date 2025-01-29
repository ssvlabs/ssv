package handlers

import (
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/networkconfig"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"

	"github.com/ssvlabs/ssv/api"
	model "github.com/ssvlabs/ssv/exporter/v2"
	trace "github.com/ssvlabs/ssv/exporter/v2/store"
	ibftstorage "github.com/ssvlabs/ssv/ibft/storage"
)

type Exporter struct {
	NetworkConfig     networkconfig.NetworkConfig
	ParticipantStores *ibftstorage.ParticipantStores
	TraceStore        trace.DutyTraceStore
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
		From       uint64   `json:"from"`
		To         uint64   `json:"to"`
		OperatorID []uint64 `json:"operatorID"`
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
		From        uint64 `json:"from"`
		To          uint64 `json:"to"`
		CommitteeID string `json:"committeeID"`
	}

	if err := api.Bind(r, &request); err != nil {
		return api.BadRequestError(err)
	}

	if request.From > request.To {
		return api.BadRequestError(fmt.Errorf("'from' must be less than or equal to 'to'"))
	}

	if len(request.CommitteeID) != 32 {
		return api.BadRequestError(fmt.Errorf("committee ID is required"))
	}

	var committeeID spectypes.CommitteeID
	copy(committeeID[:], []byte(request.CommitteeID))

	var duties []*model.CommitteeDutyTrace
	for s := request.From; s <= request.To; s++ {
		slot := phase0.Slot(s)
		duty, err := e.TraceStore.GetCommitteeDuty(slot, committeeID)
		if err != nil {
			return api.Error(fmt.Errorf("error getting duties: %w", err))
		}
		duties = append(duties, duty)
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
		From   uint64        `json:"from"`
		To     uint64        `json:"to"`
		Roles  api.RoleSlice `json:"roles"`
		VIndex uint64        `json:"index"`
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

	var results []*model.ValidatorDutyTrace

	if request.VIndex == 0 {
		for s := request.From; s <= request.To; s++ {
			slot := phase0.Slot(s)
			for _, r := range request.Roles {
				role := spectypes.BeaconRole(r)
				duties, err := e.TraceStore.GetAllValidatorDuties(role, slot)
				if err != nil {
					return api.Error(fmt.Errorf("error getting duties: %w", err))
				}
				results = append(results, duties...)
			}
		}
		return api.Render(w, r, toValidatorTraceResponse(results))
	}

	vIndex := phase0.ValidatorIndex(request.VIndex)

	for s := request.From; s <= request.To; s++ {
		slot := phase0.Slot(s)
		for _, r := range request.Roles {
			role := spectypes.BeaconRole(r)
			duty, err := e.TraceStore.GetValidatorDuty(role, slot, vIndex)
			if err != nil {
				return api.Error(fmt.Errorf("error getting duties: %w", err))
			}
			results = append(results, duty)
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
