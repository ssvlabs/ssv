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

const (
	SSVValidationStageUnknown         = "unknown"
	SSVValidationStageContext         = "context"
	SSVValidationStagePubsubBasic     = "pubsub_basic"
	SSVValidationStageDecodeSigned    = "decode_signed"
	SSVValidationStageSignedSemantics = "signed_semantics"
	SSVValidationStageSSVSemantics    = "ssv_semantics"
	SSVValidationStageCommitteeLookup = "committee_lookup"
	SSVValidationStageCommitteeChecks = "committee_checks"
	SSVValidationStageConsensus       = "consensus_validation"
	SSVValidationStagePartial         = "partial_validation"
	SSVValidationStageSignatureVerify = "signature_verification"
	SSVValidationStageStateUpdate     = "state_update"
	SSVValidationStageComplete        = "complete"
)

// SSVValidationEvent describes the SSV-level validation decision before it
// is reduced to libp2p's accept/reject/ignore validation result.
type SSVValidationEvent struct {
	PeerID         peer.ID
	Outcome        string
	Reason         string
	Stage          string
	Topic          string
	PayloadSize    int
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
	ObserveSSVValidation(context.Context, *zap.Logger, SSVValidationEvent)
}
