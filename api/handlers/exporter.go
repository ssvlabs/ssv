package handlers

import (
	"fmt"
	"net/http"

	"github.com/attestantio/go-eth2-client/spec/phase0"
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

func (e *Exporter) Decideds(w http.ResponseWriter, r *http.Request) error {
	var request struct {
		From    int           `json:"from"`
		To      int           `json:"to"`
		Roles   api.RoleSlice `json:"roles"`
		PubKeys api.HexSlice  `json:"pubkeys"`
	}
	var response struct {
		Data []*decidedJSON `json:"data"`
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

	response.Data = []*decidedJSON{}
	for _, role := range request.Roles {
		for _, pubKey := range request.PubKeys {
			for height := request.From; height <= request.To; height++ {
				identifier := spectypes.NewMsgID(e.DomainType, pubKey, spectypes.BeaconRole(role))
				storage := e.QBFTStores.Get(spectypes.BeaconRole(role))
				instance, err := storage.GetInstance(identifier[:], qbft.Height(height))
				if err != nil {
					return fmt.Errorf("error getting instance: %w", err)
				}
				if instance == nil || instance.DecidedMessage == nil {
					continue
				}
				response.Data = append(response.Data, &decidedJSON{
					Role:      role,
					Slot:      phase0.Slot(instance.DecidedMessage.Message.Height),
					PublicKey: pubKey,
					Message:   newSignedMessageJSON(instance.DecidedMessage),
				})
			}
		}
	}

	return api.Render(w, r, response)
}

type decidedJSON struct {
	Role      api.Role           `json:"role"`
	Slot      phase0.Slot        `json:"slot"`
	PublicKey api.Hex            `json:"public_key"`
	Message   *signedMessageJSON `json:"message"`
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

type signedMessageJSON struct {
	Signature spectypes.Signature
	Signers   []spectypes.OperatorID
	Message   messageJSON
	FullData  []byte
}

func newSignedMessageJSON(msg *specqbft.SignedMessage) *signedMessageJSON {
	return &signedMessageJSON{
		Signature: msg.Signature,
		Signers:   msg.Signers,
		Message: messageJSON{
			MsgType:                  msg.Message.MsgType,
			Height:                   msg.Message.Height,
			Round:                    msg.Message.Round,
			Identifier:               msg.Message.Identifier,
			Root:                     msg.Message.Root[:],
			DataRound:                msg.Message.DataRound,
			RoundChangeJustification: msg.Message.RoundChangeJustification,
			PrepareJustification:     msg.Message.PrepareJustification,
		},
		FullData: msg.FullData,
	}
}
