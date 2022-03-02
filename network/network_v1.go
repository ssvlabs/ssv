package network

import "github.com/bloxapp/ssv/protocol"

// MessageRouter is accepting network messages and route them to the corresponding (internal) components
type MessageRouter interface {
	// Route routes the given message
	Route(message protocol.SSVMessage)
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

// ValidationReporter is the interface for reporting on message validation results
type ValidationReporter interface {
	// ReportValidation reports the result for the given message
	ReportValidation(message protocol.SSVMessage, res MsgValidationResult)
}

// SubscriberV1 is the interface for subscribing to topics
type SubscriberV1 interface {
	// Subscribe subscribes to validator subnet
	Subscribe(pk protocol.ValidatorPK) error
	// UseMessageRouter registers a message router to handle incoming messages
	UseMessageRouter(router MessageRouter)
}

// BroadcasterV1 is the interface for broadcasting messages
type BroadcasterV1 interface {
	// Broadcast publishes the message to all peers in subnet
	Broadcast(message protocol.SSVMessage) error
}

// SyncerV1 is the interface for syncing messages
type SyncerV1 interface {
	// LastState fetches last state from other peers
	LastState(mid protocol.MessageID) ([]protocol.SSVMessage, error)
	// GetHistory sync the given range from other peers
	GetHistory(mid protocol.MessageID, from, to uint64) ([]protocol.SSVMessage, error)
	// LastChangeRound fetches last change round message from other peers
	LastChangeRound(mid protocol.MessageID) ([]protocol.SSVMessage, error)
	// Respond will respond on some stream with the given results
	// TODO: consider using a different approach or encapsulate the stream id
	Respond(streamID string, results []protocol.SSVMessage) error
}

// V1 is a facade interface that provides the entire functionality of the different network interfaces
type V1 interface {
	ValidationReporter
	SubscriberV1
	BroadcasterV1
	SyncerV1
}
