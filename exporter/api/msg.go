package api

import (
	"encoding/hex"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv-spec/types"
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
	Signature types.Signature
	Signers   []types.OperatorID
	Message   specqbft.Message

	FullData *types.ConsensusData
}

// NewDecidedAPIMsg creates a new message from the given message
// TODO: avoid converting to v0 once explorer is upgraded
func NewDecidedAPIMsg(msgs ...*types.SignedSSVMessage) Message {
	data, err := DecidedAPIData(msgs...)
	if err != nil {
		return Message{
			Type: TypeDecided,
			Data: []string{},
		}
	}

	decMsg, err := specqbft.DecodeMessage(msgs[0].SSVMessage.Data)
	if err != nil {
		return Message{
			Type: TypeDecided,
			Data: []string{},
		}
	}
	decMsg2, err := specqbft.DecodeMessage(msgs[len(msgs)-1].SSVMessage.Data)
	if err != nil {
		return Message{
			Type: TypeDecided,
			Data: []string{},
		}
	}

	identifier := specqbft.ControllerIdToMessageID(decMsg.Identifier)
	// #TODO fixme. its not always pk, it can be a CommitteeID if CommitteeRole or Validator PubKey
	dutyExecutorID := identifier.GetDutyExecutorID()
	role := identifier.GetRoleType()
	return Message{
		Type: TypeDecided,
		Filter: MessageFilter{
			PublicKey: hex.EncodeToString(dutyExecutorID),
			From:      uint64(decMsg.Height),
			To:        uint64(decMsg2.Height),
			Role:      role.String(),
		},
		Data: data,
	}
}

// DecidedAPIData creates a new message from the given message
func DecidedAPIData(msgs ...*types.SignedSSVMessage) (interface{}, error) {
	//if len(msgs) == 0 {
	//	return nil, errors.New("no messages")
	//}
	//
	//apiMsgs := make([]*SignedMessageAPI, 0)
	//for _, msg := range msgs {
	//	if msg == nil {
	//		return nil, errors.New("nil message")
	//	}
	//
	//	decMsg, err := specqbft.DecodeMessage(msg.SSVMessage.Data)
	//	if err != nil {
	//		return nil, err
	//	}
	//
	//	apiMsg := &SignedMessageAPI{
	//		Signature: decMsg.sog,
	//		Signers:   decMsg.Signers,
	//		Message:   msg.Message,
	//	}
	//
	//	if msg.FullData != nil {
	//		var cd types.ConsensusData
	//		if err := cd.UnmarshalSSZ(msg.FullData); err != nil {
	//			return nil, errors.Wrap(err, "failed to unmarshal consensus data")
	//		}
	//		apiMsg.FullData = &cd
	//	}
	//
	//	apiMsgs = append(apiMsgs, apiMsg)
	//}
	//
	//return apiMsgs, nil
	return nil, nil
}

// MessageFilter is a criteria for query in request messages and projection in responses
type MessageFilter struct {
	// From is the starting index of the desired data
	From uint64 `json:"from"`
	// To is the ending index of the desired data
	To uint64 `json:"to"`
	// Role is the duty type, optional as it's relevant for IBFT data
	Role string `json:"role,omitempty"`
	// PublicKey is optional, used for fetching decided messages or information about specific validator/operator
	PublicKey string `json:"publicKey,omitempty"`
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
)
