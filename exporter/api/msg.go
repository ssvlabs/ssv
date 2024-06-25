package api

import (
	"encoding/hex"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
)

// Message represents an exporter message
type Message struct {
	// Type is the type of message
	Type MessageType `json:"type"`
	// Filter
	Filter MessageFilter `json:"filter"`
	// Values holds the results, optional as it's relevant for response
	Data interface{} `json:"data,omitempty"`
}

type SignedMessageAPI struct {
	Signature spectypes.Signature
	Signers   []spectypes.OperatorID
	Message   specqbft.Message

	FullData *spectypes.ConsensusData
}

type ParticipantsAPI struct {
	Signers     []spectypes.OperatorID
	Slot        phase0.Slot
	Identifier  []byte
	ValidatorPK string
	Role        string
	Message     specqbft.Message
	FullData    *spectypes.ConsensusData
}

// NewParticipantsAPIMsg creates a new message in a new format from the given message.
func NewParticipantsAPIMsg(msgs ...qbftstorage.ParticipantsRangeEntry) Message {
	data, err := ParticipantsAPIData(msgs...)
	if err != nil {
		return Message{
			Type: TypeParticipants,
			Data: []string{},
		}
	}
	identifier := specqbft.ControllerIdToMessageID(msgs[0].Identifier[:])
	pkv := identifier.GetDutyExecutorID()

	var publicKeys []string
	publicKeys = append(publicKeys, hex.EncodeToString(pkv))
	return Message{
		Type: TypeDecided,
		Filter: MessageFilter{
			PublicKeys: publicKeys,
			From:       uint64(msgs[0].Slot),
			To:         uint64(msgs[len(msgs)-1].Slot),
			Role:       msgs[0].Identifier.GetRoleType().ToBeaconRole(),
		},
		Data: data,
	}
}

// ParticipantsAPIData creates a new message from the given message in a new format.
func ParticipantsAPIData(msgs ...qbftstorage.ParticipantsRangeEntry) (interface{}, error) {
	if len(msgs) == 0 {
		return nil, errors.New("no messages")
	}

	apiMsgs := make([]*ParticipantsAPI, 0)
	for _, msg := range msgs {
		apiMsg := &ParticipantsAPI{
			Signers:     msg.Signers,
			Slot:        msg.Slot,
			Identifier:  msg.Identifier[:],
			ValidatorPK: hex.EncodeToString(msg.Identifier.GetDutyExecutorID()),
			Role:        msg.Identifier.GetRoleType().ToBeaconRole(),
			Message: specqbft.Message{
				MsgType:    specqbft.CommitMsgType,
				Height:     specqbft.Height(msg.Slot),
				Identifier: msg.Identifier[:],
				Round:      specqbft.FirstRound,
			},
			FullData: &spectypes.ConsensusData{Duty: spectypes.BeaconDuty{Slot: msg.Slot}},
		}

		apiMsgs = append(apiMsgs, apiMsg)
	}

	return apiMsgs, nil
}

// MessageFilter is a criteria for query in request messages and projection in responses
type MessageFilter struct {
	// From is the starting index of the desired data
	From uint64 `json:"from"`
	// To is the ending index of the desired data
	To uint64 `json:"to"`
	// Role is the duty type, optional as it's relevant for IBFT data
	Role string `json:"role,omitempty"`
	// PublicKeys is optional, used for fetching decided messages or information about specific validator/operator
	PublicKeys []string `json:"publicKeys,omitempty"`
}

// MessageType is the type of message being sent
type MessageType string

const (
	// TypeValidator is an enum for validator type messages
	TypeValidator MessageType = "validator"
	// TypeOperator is an enum for operator type messages
	TypeOperator MessageType = "operator"
	// TypeDecided is an enum for ibft type messages
	TypeDecided MessageType = "decided"
	// TypeError is an enum for error type messages
	TypeError MessageType = "error"
	// TypeParticipants is an enum for participants type messages
	TypeParticipants MessageType = "participants"
)
