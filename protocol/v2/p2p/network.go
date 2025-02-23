package protocolp2p

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ssvlabs/ssv-spec/p2p"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
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
}

// SyncResult holds the result of a sync request, including the actual message and the sender
type SyncResult struct {
	Msg    *spectypes.SSVMessage
	Sender string
}

type SyncResults []SyncResult

// String method was created for logging purposes
func (s SyncResults) String() string {
	var v []string
	for _, m := range s {
		var sm *spectypes.SignedSSVMessage
		if m.Msg.MsgType == spectypes.SSVConsensusMsgType {

			decMsg, err := specqbft.DecodeMessage(sm.SSVMessage.Data)
			if err != nil {
				v = append(v, fmt.Sprintf("(%v)", err))
				continue
			}

			v = append(
				v,
				fmt.Sprintf(
					"(type=%d height=%d round=%d)",
					m.Msg.MsgType,
					decMsg.Height,
					decMsg.Round,
				),
			)
		}
		v = append(v, fmt.Sprintf("(type=%d)", m.Msg.MsgType))
	}

	return strings.Join(v, ", ")
}

// SyncProtocol represent the type of sync protocols
type SyncProtocol int32

const (
	// LastDecidedProtocol is the last decided protocol type
	LastDecidedProtocol SyncProtocol = iota
	// DecidedHistoryProtocol is the decided history protocol type
	DecidedHistoryProtocol
)

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
