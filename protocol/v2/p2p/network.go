package protocolp2p

import (
	"errors"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv-spec/p2p"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var (
	// ErrNetworkIsNotReady is returned when trying to access the network instance before it's ready
	ErrNetworkIsNotReady = errors.New("network services are not ready")
)

// Subscriber manages topics subscription
type Subscriber interface {
	p2p.Subscriber
	// Unsubscribe unsubscribes from the validator subnet
	Unsubscribe(pk spectypes.ValidatorPK) error
	// Peers returns the peers that are connected to the given validator
}

// Broadcaster enables to broadcast messages
type Broadcaster interface {
	Broadcast(id spectypes.MessageID, message *spectypes.SignedSSVMessage) error
	// BroadcastAtSlot behaves like Broadcast but takes the message's slot explicitly, which
	// determines whether it's sent on Alan or Boole topics (see networkconfig.Network.BooleForkAtSlot).
	BroadcastAtSlot(message *spectypes.SignedSSVMessage, slot phase0.Slot) error
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
	ReportValidation(message *spectypes.SSVMessage, res MsgValidationResult)
}

// Network holds the networking layer used to complement the underlying protocols
type Network interface {
	Subscriber
	Broadcaster
	ValidationReporting
}
