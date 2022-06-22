package api

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/ibft/conversion"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/utils/format"
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

// NewDecidedAPIMsg creates a new message from the given message
// TODO: avoid converting to v0 once explorer is upgraded
func NewDecidedAPIMsg(msg *message.SignedMessage) (Message, error) {
	pkv := msg.Message.Identifier.GetValidatorPK()
	identifierV0 := format.IdentifierFormat(pkv, msg.Message.Identifier.GetRoleType().String())
	v0Msg, err := conversion.ToSignedMessageV0(msg, []byte(identifierV0))
	if err != nil {
		return Message{}, err
	}
	return Message{
		Type: TypeDecided,
		Filter: MessageFilter{
			PublicKey: hex.EncodeToString(pkv),
			From:      uint64(msg.Message.Height),
			To:        uint64(msg.Message.Height),
			Role:      DutyRole(msg.Message.Identifier.GetRoleType().String()),
		},
		Data: []*proto.SignedMessage{v0Msg},
	}, nil
}

// MessageFilter is a criteria for query in request messages and projection in responses
type MessageFilter struct {
	// From is the starting index of the desired data
	From uint64 `json:"from"`
	// To is the ending index of the desired data
	To uint64 `json:"to"`
	// Role is the duty type enum, optional as it's relevant for IBFT data
	Role DutyRole `json:"role,omitempty"`
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

// DutyRole is the role of the duty
type DutyRole string

const (
	// RoleAttester is an enum for attester role
	RoleAttester DutyRole = "ATTESTER"
	// RoleAggregator is an enum for aggregator role
	RoleAggregator DutyRole = "AGGREGATOR"
	// RoleProposer is an enum for proposer role
	RoleProposer DutyRole = "PROPOSER"
)
