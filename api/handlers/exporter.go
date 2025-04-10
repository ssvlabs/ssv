package handlers

import (
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/api"
	ibftstorage "github.com/ssvlabs/ssv/ibft/storage"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
)

type Exporter struct {
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
