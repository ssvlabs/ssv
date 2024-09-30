package handlers

import (
	"fmt"
	"net/http"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/api"
	exporterapi "github.com/ssvlabs/ssv/exporter/api"
	"github.com/ssvlabs/ssv/exporter/convert"
	ibftstorage "github.com/ssvlabs/ssv/ibft/storage"
)

type Exporter struct {
	DomainType spectypes.DomainType
	QBFTStores *ibftstorage.QBFTStores
}

func (e *Exporter) Decideds(w http.ResponseWriter, r *http.Request) error {
	var request struct {
		From    int           `json:"from"`
		To      int           `json:"to"`
		Roles   api.RoleSlice `json:"roles"`
		PubKeys api.HexSlice  `json:"pubkeys"`
	}
	var response struct {
		Data []*exporterapi.ParticipantsAPI `json:"data"`
	}

	if err := api.Bind(r, &request); err != nil {
		return err
	}

	if request.From > request.To {
		return api.BadRequestError(fmt.Errorf("from must be less than to"))
	}
	if request.From < 0 || request.To < 0 {
		return api.BadRequestError(fmt.Errorf("from and to must be greater than 0"))
	}
	if len(request.PubKeys) == 0 {
		return api.BadRequestError(fmt.Errorf("at least one public key is required"))
	}
	if len(request.Roles) == 0 {
		return api.BadRequestError(fmt.Errorf("at least one role is required"))
	}

	response.Data = []*exporterapi.ParticipantsAPI{}
	for _, role := range request.Roles {
		roleStorage := e.QBFTStores.Get(convert.RunnerRole(role))
		if roleStorage == nil {
			return fmt.Errorf("role storage doesn't exist: %v", role)
		}
		for _, pubKey := range request.PubKeys {
			msgID := convert.NewMsgID(e.DomainType, pubKey, convert.RunnerRole(role))
			from := phase0.Slot(request.From)
			to := phase0.Slot(request.To)

			participantsList, err := roleStorage.GetParticipantsInRange(msgID, from, to)
			if err != nil {
				return fmt.Errorf("error getting participants: %w", err)
			}

			data, err := exporterapi.ParticipantsAPIData(participantsList...)
			if err != nil {
				return fmt.Errorf("error getting participants API data: %w", err)
			}

			apiData, ok := data.([]*exporterapi.ParticipantsAPI)
			if !ok {
				return fmt.Errorf("invalid type for participants API data")
			}

			response.Data = append(response.Data, apiData...)
		}
	}

	return api.Render(w, r, response)
}
