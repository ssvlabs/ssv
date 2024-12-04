package handlers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/api"
	exporterapi "github.com/ssvlabs/ssv/exporter/api"
	"github.com/ssvlabs/ssv/exporter/convert"
	ibftstorage "github.com/ssvlabs/ssv/ibft/storage"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	"github.com/ssvlabs/ssv/utils/casts"
)

type Exporter struct {
	DomainType spectypes.DomainType
	QBFTStores *ibftstorage.QBFTStores
	Log        *zap.Logger
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

	start := time.Now()
	dbTime := time.Duration(0)

	defer func() {
		e.Log.Debug("decideds", zap.Duration("total", time.Since(start)), zap.Duration("db", dbTime))
	}()

	response.Data = []*ParticipantResponse{}

	qbftStores := make(map[convert.RunnerRole]qbftstorage.QBFTStore, len(request.Roles))
	for _, role := range request.Roles {
		runnerRole := casts.BeaconRoleToConvertRole(spectypes.BeaconRole(role))
		storage := e.QBFTStores.Get(runnerRole)
		if storage == nil {
			return api.Error(fmt.Errorf("role storage doesn't exist: %v", role))
		}

		qbftStores[runnerRole] = storage
	}

	from := phase0.Slot(request.From)
	to := phase0.Slot(request.To)

	for _, role := range request.Roles {
		runnerRole := casts.BeaconRoleToConvertRole(spectypes.BeaconRole(role))
		qbftStore := qbftStores[runnerRole]

		for _, pubKey := range request.PubKeys {
			msgID := convert.NewMsgID(e.DomainType, pubKey, runnerRole)

			dbStart := time.Now()
			participantsList, err := qbftStore.GetParticipantsInRange(msgID, from, to)
			if err != nil {
				return api.Error(fmt.Errorf("error getting participants: %w", err))
			}
			dbTime += time.Since(dbStart)

			if len(participantsList) == 0 {
				continue
			}

			data, err := exporterapi.ParticipantsAPIData(participantsList...)
			if err != nil {
				return api.Error(fmt.Errorf("error getting participants API data: %w", err))
			}

			for _, apiMsg := range data {
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
