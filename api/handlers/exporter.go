package handlers

import (
	"fmt"
	"net/http"

	"github.com/bloxapp/ssv-spec/qbft"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/api"
	ibftstorage "github.com/bloxapp/ssv/ibft/storage"
)

type Exporter struct {
	DomainType spectypes.DomainType
	QBFTStores *ibftstorage.QBFTStores
}

func (e *Exporter) Instances(w http.ResponseWriter, r *http.Request) error {
	var request struct {
		From    int           `json:"from"`
		To      int           `json:"to"`
		Roles   api.RoleSlice `json:"roles"`
		PubKeys api.HexSlice  `json:"pubkeys"`
	}
	var response struct {
		Data []*signedMessageJSON `json:"data"`
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

	response.Data = []*signedMessageJSON{}
	for _, pubKey := range request.PubKeys {
		for height := request.From; height <= request.To; height++ {
			for _, role := range request.Roles {
				identifier := spectypes.NewMsgID(e.DomainType, pubKey, spectypes.BeaconRole(role))
				storage := e.QBFTStores.Get(spectypes.BeaconRole(role))
				instance, err := storage.GetInstance(identifier[:], qbft.Height(height))
				if err != nil {
					return api.BadRequestError(fmt.Errorf("error getting instance: %w", err))
				}
				if instance == nil || instance.DecidedMessage == nil {
					continue
				}
				response.Data = append(response.Data, &signedMessageJSON{
					Signature: instance.DecidedMessage.Signature,
					Signers:   instance.DecidedMessage.Signers,
					Message: messageJSON{
						MsgType:                  instance.DecidedMessage.Message.MsgType,
						Height:                   instance.DecidedMessage.Message.Height,
						Round:                    instance.DecidedMessage.Message.Round,
						Identifier:               instance.DecidedMessage.Message.Identifier,
						Root:                     instance.DecidedMessage.Message.Root[:],
						DataRound:                instance.DecidedMessage.Message.DataRound,
						RoundChangeJustification: instance.DecidedMessage.Message.RoundChangeJustification,
						PrepareJustification:     instance.DecidedMessage.Message.PrepareJustification,
					},
					FullData: instance.DecidedMessage.FullData,
				})
			}
		}
	}

	return api.Render(w, r, response)
}

type signedMessageJSON struct {
	Signature spectypes.Signature
	Signers   []spectypes.OperatorID
	Message   messageJSON
	FullData  []byte
}

type messageJSON struct {
	MsgType    specqbft.MessageType
	Height     specqbft.Height
	Round      specqbft.Round
	Identifier []byte

	Root                     []byte
	DataRound                specqbft.Round
	RoundChangeJustification [][]byte
	PrepareJustification     [][]byte
}
