package protcolp2p

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
)

// Subscriber manages topics subscription
type Subscriber interface {
	// Subscribe subscribes to validator subnet
	Subscribe(pk message.ValidatorPK) error
	// Unsubscribe unsubscribes from the validator subnet
	Unsubscribe(pk message.ValidatorPK) error
}

// Broadcaster enables to broadcast messages
type Broadcaster interface {
	// Broadcast broadcasts the given message to the corresponding subnet
	Broadcast(msg message.SSVMessage) error
}

// RequestHandler handles p2p requests
type RequestHandler func(*message.SSVMessage) (*message.SSVMessage, error)

// SyncResult holds the result of a sync request, including the actual message and the sender
type SyncResult struct {
	Msg    *message.SSVMessage
	Sender string
}

// Syncer holds the interface for syncing data from other peerz
type Syncer interface {
	// RegisterHandler registers handler for the given protocol
	RegisterHandler(protocol string, handler RequestHandler)
	// LastDecided fetches last decided from a random set of peers
	LastDecided(mid message.Identifier) ([]SyncResult, error)
	// GetHistory sync the given range from a set of peers that supports history for the given identifier
	// it accepts a list of targets for the request
	GetHistory(mid message.Identifier, from, to message.Height, targets ...string) ([]SyncResult, error)
	// LastChangeRound fetches last change round message from a random set of peers
	LastChangeRound(mid message.Identifier, height message.Height) ([]SyncResult, error)
}

// MsgValidationResult helps other components to report message validation with a generic results scheme
type MsgValidationResult int32

const (
	// ValidationAccept is the result of a valid message
	ValidationAccept MsgValidationResult = iota
	// ValidationIgnore is the result in case we want to ignore the validation
	ValidationIgnore
	// ValidationRejectLow is the result for invalid message, with low severity (e.g. late message)
	ValidationRejectLow
	// ValidationRejectMedium is the result for invalid message, with medium severity (e.g. wrong height)
	ValidationRejectMedium
	// ValidationRejectHigh is the result for invalid message, with high severity (e.g. invalid signature)
	ValidationRejectHigh
)

// ValidationReporting is the interface for reporting on message validation results
type ValidationReporting interface {
	// ReportValidation reports the result for the given message
	ReportValidation(message *message.SSVMessage, res MsgValidationResult)
}

// Network holds the networking layer used to complement the underlying protocols
type Network interface {
	Subscriber
	Broadcaster
	Syncer
	ValidationReporting
}
