package msgvalidation

import (
	"errors"
	"fmt"
	"strings"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
)

type Error struct {
	text     string
	got      any
	want     any
	innerErr error
	reject   bool
	silent   bool
}

func (e Error) Error() string {
	var sb strings.Builder
	sb.WriteString(e.text)

	if e.got != nil {
		sb.WriteString(fmt.Sprintf(", got %v", e.got))
	}
	if e.want != nil {
		sb.WriteString(fmt.Sprintf(", want %v", e.want))
	}
	if e.innerErr != nil {
		sb.WriteString(fmt.Sprintf(": %s", e.innerErr.Error()))
	}

	return sb.String()
}

func (e Error) Reject() bool {
	return e.reject
}

func (e Error) Silent() bool {
	return e.silent
}

func (e Error) Text() string {
	return e.text
}

var (
	ErrEmptyData                              = Error{text: "empty data"}
	ErrWrongDomain                            = Error{text: "wrong domain", silent: true}
	ErrNoShareMetadata                        = Error{text: "share has no metadata"}
	ErrUnknownValidator                       = Error{text: "unknown validator"}
	ErrValidatorLiquidated                    = Error{text: "validator is liquidated"}
	ErrValidatorNotAttesting                  = Error{text: "validator is not attesting"}
	ErrSlotAlreadyAdvanced                    = Error{text: "signer has already advanced to a later slot"}
	ErrRoundAlreadyAdvanced                   = Error{text: "signer has already advanced to a later round"}
	ErrRoundTooHigh                           = Error{text: "round is too high for this role" /*, reject: true*/} // TODO: enable reject
	ErrEarlyMessage                           = Error{text: "early message"}
	ErrLateMessage                            = Error{text: "late message"}
	ErrTooManySameTypeMessagesPerRound        = Error{text: "too many messages of same type per round"}
	ErrSignatureVerification                  = Error{text: "signature verification", reject: true}
	ErrOperatorNotFound                       = Error{text: "operator not found", reject: true}
	ErrPubSubMessageHasNoData                 = Error{text: "pub-sub message has no data", reject: true}
	ErrPubSubDataTooBig                       = Error{text: "pub-sub message data too big", reject: true}
	ErrMalformedPubSubMessage                 = Error{text: "pub-sub message is malformed", reject: true}
	ErrEmptyPubSubMessage                     = Error{text: "pub-sub message is empty", reject: true}
	ErrNilSSVMessage                          = Error{text: "ssv message is nil", reject: true}
	ErrSignedSSVMessageValidation             = Error{text: "signed ssv message invalid", reject: true}
	ErrTopicNotFound                          = Error{text: "topic not found", reject: true}
	ErrSSVDataTooBig                          = Error{text: "ssv message data too big", reject: true}
	ErrInvalidRole                            = Error{text: "invalid role", reject: true}
	ErrUnexpectedConsensusMessage             = Error{text: "unexpected consensus message for this role", reject: true}
	ErrNoSigners                              = Error{text: "no signers", reject: true}
	ErrWrongSignatureSize                     = Error{text: "wrong signature size", reject: true}
	ErrZeroSignature                          = Error{text: "zero signature", reject: true}
	ErrZeroSigner                             = Error{text: "zero signer ID", reject: true}
	ErrSignerNotInCommittee                   = Error{text: "signer is not in committee", reject: true}
	ErrDuplicatedSigner                       = Error{text: "signer is duplicated", reject: true}
	ErrSignerNotLeader                        = Error{text: "signer is not leader", reject: true}
	ErrSignersNotSorted                       = Error{text: "signers are not sorted", reject: true}
	ErrUnexpectedSigner                       = Error{text: "signer is not expected", reject: true}
	ErrInvalidHash                            = Error{text: "root doesn't match full data hash", reject: true}
	ErrEstimatedRoundTooFar                   = Error{text: "message round is too far from estimated"}
	ErrMalformedMessage                       = Error{text: "message could not be decoded", reject: true}
	ErrWrongSSVMessageType                    = Error{text: "wrong SSV message type", reject: true}
	ErrUnknownQBFTMessageType                 = Error{text: "unknown QBFT message type", reject: true}
	ErrUnknownPartialMessageType              = Error{text: "unknown partial signature message type", reject: true}
	ErrPartialSignatureTypeRoleMismatch       = Error{text: "partial signature type and role don't match", reject: true}
	ErrNonDecidedWithMultipleSigners          = Error{text: "non-decided with multiple signers", reject: true}
	ErrWrongSignersLength                     = Error{text: "decided signers size is not between quorum and committee size", reject: true}
	ErrDuplicatedProposalWithDifferentData    = Error{text: "duplicated proposal with different data", reject: true}
	ErrMalformedPrepareJustifications         = Error{text: "malformed prepare justifications", reject: true}
	ErrUnexpectedPrepareJustifications        = Error{text: "prepare justifications unexpected for this message type", reject: true}
	ErrMalformedRoundChangeJustifications     = Error{text: "malformed round change justifications", reject: true}
	ErrUnexpectedRoundChangeJustifications    = Error{text: "round change justifications unexpected for this message type", reject: true}
	ErrTooManyDutiesPerEpoch                  = Error{text: "too many duties per epoch", reject: true}
	ErrNoDuty                                 = Error{text: "no duty for this epoch", reject: true}
	ErrDeserializePublicKey                   = Error{text: "deserialize public key", reject: true}
	ErrNoPartialMessages                      = Error{text: "no partial messages", reject: true}
	ErrDuplicatedPartialSignatureMessage      = Error{text: "duplicated partial signature message", reject: true}
	ErrNonExistingCommitteeID                 = Error{text: "cluster ID doesn't exist", reject: true}
	ErrNoValidators                           = Error{text: "no validators for this committee ID", reject: true}
	ErrPartialSignatureValidatorIndexNotFound = Error{text: "partial signature validator index not found", reject: true}
)

func (mv *messageValidator) handleValidationError(peerID peer.ID, decodedMessage *DecodedMessage, err error) pubsub.ValidationResult {
	loggerFields := mv.buildLoggerFields(decodedMessage)

	logger := mv.logger.
		With(loggerFields.AsZapFields()...).
		With(fields.PeerID(peerID))

	var valErr Error
	if !errors.As(err, &valErr) {
		mv.metrics.MessageIgnored(err.Error(), loggerFields.Role, loggerFields.Consensus.Round)
		logger.Debug("ignoring invalid message", zap.Error(err))
		return pubsub.ValidationIgnore
	}

	if !valErr.Reject() {
		if !valErr.Silent() {
			logger.Debug("ignoring invalid message", zap.Error(valErr))
		}
		mv.metrics.MessageIgnored(valErr.Text(), loggerFields.Role, loggerFields.Consensus.Round)
		return pubsub.ValidationIgnore
	}

	if !valErr.Silent() {
		logger.Debug("rejecting invalid message", zap.Error(valErr))
	}

	mv.metrics.MessageRejected(valErr.Text(), loggerFields.Role, loggerFields.Consensus.Round)
	return pubsub.ValidationReject
}
