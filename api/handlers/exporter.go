package handlers

import (
	"fmt"
	"net/http"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"

	"github.com/ssvlabs/ssv/api"
	exporterapi "github.com/ssvlabs/ssv/exporter/api"
	ibftstorage "github.com/ssvlabs/ssv/ibft/storage"
)

type Exporter struct {
	DomainType        spectypes.DomainType
	ParticipantStores *ibftstorage.ParticipantStores
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

	if len(request.PubKeys) == 0 {
		return api.BadRequestError(fmt.Errorf("at least one public key is required"))
	}

	if len(request.Roles) == 0 {
		return api.BadRequestError(fmt.Errorf("at least one role is required"))
	}

	response.Data = []*ParticipantResponse{}

	for _, role := range request.Roles {
		participanStore := e.ParticipantStores.Get(spectypes.BeaconRole(role))

		for _, pubKey := range request.PubKeys {
			from := phase0.Slot(request.From)
			to := phase0.Slot(request.To)

			participantsRange, err := participanStore.GetParticipantsInRange(spectypes.BeaconRole(role), spectypes.ValidatorPK(pubKey), from, to)
			if err != nil {
				return api.Error(fmt.Errorf("error getting participants: %w", err))
			}

			if len(participantsRange) == 0 {
				continue
			}

			participantsList := make([]qbftstorage.Participation, 0, len(participantsRange))

			for _, p := range participantsRange {
				participantsList = append(participantsList, qbftstorage.Participation{
					ParticipantsRangeEntry: p,

					DomainType: e.DomainType,
					Role:       spectypes.BeaconRole(role),
					PK:         spectypes.ValidatorPK(pubKey),
				})
			}

			data, err := exporterapi.ParticipantsAPIData(participantsList...)
			if err != nil {
				return api.Error(fmt.Errorf("error getting participants API data: %w", err))
			}

			apiData, ok := data.([]*exporterapi.ParticipantsAPI)
			if !ok {
				return api.Error(fmt.Errorf("invalid type for participants API data"))
			}

			for _, apiMsg := range apiData {
				response.Data = append(response.Data, transformToParticipantResponse(apiMsg))
			}
		}
	}

	return api.Render(w, r, response)
}

func transformToParticipantResponse(apiMsg *exporterapi.ParticipantsAPI) *ParticipantResponse {
	response := &ParticipantResponse{
		Role:      apiMsg.Role,
		Slot:      uint64(apiMsg.Slot),
		PublicKey: apiMsg.ValidatorPK,
	}
	response.Message.Signers = apiMsg.Signers

	return response
}
