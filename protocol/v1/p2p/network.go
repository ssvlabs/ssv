package protcolp2p

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/libp2p/go-libp2p-core/peer"
)

// Subscriber manages topics subscription
type Subscriber interface {
	// Subscribe subscribes to validator subnet
	Subscribe(pk message.ValidatorPK) error
	// Unsubscribe unsubscribes from the validator subnet
	Unsubscribe(pk message.ValidatorPK) error
	// Peers returns the peers that are connected to the given validator
	Peers(pk message.ValidatorPK) ([]peer.ID, error)
}

// Broadcaster enables to broadcast messages
type Broadcaster interface {
	// Broadcast broadcasts the given message to the corresponding subnet
	Broadcast(msg message.SSVMessage) error
}

// RequestHandler handles p2p requests
type RequestHandler func(*message.SSVMessage) (*message.SSVMessage, error)

// CombineRequestHandlers combines multiple handlers into a single handler
func CombineRequestHandlers(handlers ...RequestHandler) RequestHandler {
	return func(ssvMessage *message.SSVMessage) (res *message.SSVMessage, err error) {
		for _, handler := range handlers {
			res, err = handler(ssvMessage)
			if err != nil {
				return nil, err
			}
			if res != nil {
				return res, nil
			}
		}
		return
	}
}

// SyncResult holds the result of a sync request, including the actual message and the sender
type SyncResult struct {
	Msg    *message.SSVMessage
	Sender string
}

// SyncProtocol represent the type of sync protocols
type SyncProtocol int32

const (
	// LastDecidedProtocol is the last decided protocol type
	LastDecidedProtocol SyncProtocol = iota
	// LastChangeRoundProtocol is the last change round protocol type
	LastChangeRoundProtocol
	// DecidedHistoryProtocol is the decided history protocol type
	DecidedHistoryProtocol
)

type HandlerOpt struct {
	Protocol SyncProtocol
	Handler  RequestHandler
}

func WithHandler(protocol SyncProtocol, handler RequestHandler) *HandlerOpt {
	return &HandlerOpt{
		Protocol: protocol,
		Handler:  handler,
	}
}

// Syncer holds the interface for syncing data from other peerz
type Syncer interface {
	// RegisterHandlers registers handler for the given protocol
	RegisterHandlers(handlers ...*HandlerOpt)
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
