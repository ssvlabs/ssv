package api

import (
	"github.com/bloxapp/ssv/exporter/storage"
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

// ValidatorsMessage represents message for validators response
type ValidatorsMessage struct {
	Data []storage.ValidatorInformation `json:"data,omitempty"`
}

// OperatorsMessage represents message for operators response
type OperatorsMessage struct {
	Data []storage.OperatorInformation `json:"data,omitempty"`
}
