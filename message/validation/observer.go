package validation

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const (
	SSVValidationAccepted = "accepted"
	SSVValidationIgnored  = "ignored"
	SSVValidationRejected = "rejected"
)

// SSVValidationEvent describes the SSV-level validation decision before it
// is reduced to libp2p's accept/reject/ignore validation result.
// Its pointer and slice fields are shared with the validator, so the event is
// only valid for the duration of the ObserveSSVValidation call.
type SSVValidationEvent struct {
	PeerID         peer.ID
	Outcome        string
	Reason         string
	Role           spectypes.RunnerRole
	SSVMessageType spectypes.MsgType
	Slot           phase0.Slot
	DutyExecutorID []byte
	Signers        []spectypes.OperatorID
	Consensus      *ConsensusFields
	Error          string
}

// SSVValidationObserver receives SSV-level validation decisions.
type SSVValidationObserver interface {
	// Interested reports whether the observer wants events for messages received
	// from the given peer. It must be cheap: it gates event construction on the
	// message validation hot path.
	Interested(peer.ID) bool
	ObserveSSVValidation(context.Context, *zap.Logger, SSVValidationEvent)
}
