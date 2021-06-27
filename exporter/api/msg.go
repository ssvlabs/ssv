package api

import (
	"github.com/bloxapp/ssv/ibft/proto"
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

// MessageFilter is a criteria for query in request messages and projection in responses
type MessageFilter struct {
	// From is the starting index of the desired data
	From int64 `json:"from"`
	// To is the ending index of the desired data
	To int64 `json:"to"`
	// Role is the duty type enum, optional as it's relevant for IBFT data
	Role DutyRole `json:"role,omitempty"`
	// PubKey is optional as it's relevant for IBFT data
	PubKey string `json:"pubKey,omitempty"`
}

// MessageType is the type of message being sent
type MessageType string

const (
	// TypeValidator is an enum for validator type
	TypeValidator MessageType = "validator"
	// TypeOperator is an enum for validator type
	TypeOperator MessageType = "operator"
	// TypeIBFT is an enum for validator type
	TypeIBFT MessageType = "ibft"
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

// ValidatorMsg represents a transferable object
type ValidatorMsg struct {
	Index     int64                  `json:"index"`
	PublicKey string                 `json:"publicKey"`
	Committee map[uint64]*proto.Node `json:"operators"`
}
