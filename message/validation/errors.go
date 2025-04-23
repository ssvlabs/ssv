package validation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
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

func (e Error) Unwrap() error {
	return e.innerErr
}

func (e Error) Is(target error) bool {
	var t Error
	if !errors.As(target, &t) {
		return false
	}

	return e.text == t.text
}

var (
	ErrWrongDomain                             = Error{text: "wrong domain"}
	ErrNoShareMetadata                         = Error{text: "share has no metadata"}
	ErrUnknownValidator                        = Error{text: "unknown validator"}
	ErrValidatorLiquidated                     = Error{text: "validator is liquidated"}
	ErrValidatorNotAttesting                   = Error{text: "validator is not attesting"}
	ErrEarlySlotMessage                        = Error{text: "message was sent before slot starts"}
	ErrLateSlotMessage                         = Error{text: "current time is above duty's start +34(committee and aggregator) or +3(else) slots"}
	ErrSlotAlreadyAdvanced                     = Error{text: "signer has already advanced to a later slot"}
	ErrRoundAlreadyAdvanced                    = Error{text: "signer has already advanced to a later round"}
	ErrDecidedWithSameSigners                  = Error{text: "decided with same number of signers"}
	ErrPubSubDataTooBig                        = Error{text: "pub-sub message data too big"}
	ErrIncorrectTopic                          = Error{text: "incorrect topic"}
	ErrNonExistentCommitteeID                  = Error{text: "committee ID doesn't exist"}
	ErrRoundTooHigh                            = Error{text: "round is too high for this role"}
	ErrValidatorIndexMismatch                  = Error{text: "partial signature validator index not found"}
	ErrTooManyDutiesPerEpoch                   = Error{text: "too many duties per epoch"}
	ErrNoDuty                                  = Error{text: "no duty for this epoch"}
	ErrEstimatedRoundNotInAllowedSpread        = Error{text: "message round is too far from estimated"}
	ErrEmptyData                               = Error{text: "empty data", reject: true}
	ErrMismatchedIdentifier                    = Error{text: "identifier mismatch", reject: true}
	ErrSignatureVerification                   = Error{text: "signature verification", reject: true}
	ErrPubSubMessageHasNoData                  = Error{text: "pub-sub message has no data", reject: true}
	ErrMalformedPubSubMessage                  = Error{text: "pub-sub message is malformed", reject: true}
	ErrNilSignedSSVMessage                     = Error{text: "signed ssv message is nil", reject: true}
	ErrNilSSVMessage                           = Error{text: "ssv message is nil", reject: true}
	ErrSSVDataTooBig                           = Error{text: "ssv message data too big", reject: true}
	ErrInvalidRole                             = Error{text: "invalid role", reject: true}
	ErrUnexpectedConsensusMessage              = Error{text: "unexpected consensus message for this role", reject: true}
	ErrNoSigners                               = Error{text: "no signers", reject: true}
	ErrWrongRSASignatureSize                   = Error{text: "wrong RSA signature size", reject: true}
	ErrZeroSigner                              = Error{text: "zero signer ID", reject: true}
	ErrSignerNotInCommittee                    = Error{text: "signer is not in committee", reject: true}
	ErrDuplicatedSigner                        = Error{text: "signer is duplicated", reject: true}
	ErrSignerNotLeader                         = Error{text: "signer is not leader", reject: true}
	ErrSignersNotSorted                        = Error{text: "signers are not sorted", reject: true}
	ErrInconsistentSigners                     = Error{text: "signer is not expected", reject: true}
	ErrInvalidHash                             = Error{text: "root doesn't match full data hash", reject: true}
	ErrFullDataHash                            = Error{text: "couldn't hash root", reject: true}
	ErrUndecodableMessageData                  = Error{text: "message data could not be decoded", reject: true}
	ErrEventMessage                            = Error{text: "unexpected event message", reject: true}
	ErrUnknownSSVMessageType                   = Error{text: "unknown SSV message type", reject: true}
	ErrUnknownQBFTMessageType                  = Error{text: "unknown QBFT message type", reject: true}
	ErrInvalidPartialSignatureType             = Error{text: "unknown partial signature message type", reject: true}
	ErrPartialSignatureTypeRoleMismatch        = Error{text: "partial signature type and role don't match", reject: true}
	ErrNonDecidedWithMultipleSigners           = Error{text: "non-decided with multiple signers", reject: true}
	ErrDecidedNotEnoughSigners                 = Error{text: "not enough signers in decided message", reject: true}
	ErrDifferentProposalData                   = Error{text: "different proposal data", reject: true}
	ErrMalformedPrepareJustifications          = Error{text: "malformed prepare justifications", reject: true}
	ErrUnexpectedPrepareJustifications         = Error{text: "prepare justifications unexpected for this message type", reject: true}
	ErrMalformedRoundChangeJustifications      = Error{text: "malformed round change justifications", reject: true}
	ErrUnexpectedRoundChangeJustifications     = Error{text: "round change justifications unexpected for this message type", reject: true}
	ErrNoPartialSignatureMessages              = Error{text: "no partial signature messages", reject: true}
	ErrNoValidators                            = Error{text: "no validators for this committee ID", reject: true}
	ErrNoSignatures                            = Error{text: "no signatures", reject: true}
	ErrSignersAndSignaturesWithDifferentLength = Error{text: "signature and operator ID length mismatch", reject: true}
	ErrPartialSigOneSigner                     = Error{text: "partial signature message must have only one signer", reject: true}
	ErrPrepareOrCommitWithFullData             = Error{text: "prepare or commit with full data", reject: true}
	ErrFullDataNotInConsensusMessage           = Error{text: "full data not in consensus message", reject: true}
	ErrTripleValidatorIndexInPartialSignatures = Error{text: "triple validator index in partial signatures", reject: true}
	ErrZeroRound                               = Error{text: "zero round", reject: true}
	ErrDuplicatedMessage                       = Error{text: "message is duplicated", reject: true}
	ErrInvalidPartialSignatureTypeCount        = Error{text: "sent more partial signature messages of a certain type than allowed", reject: true}
	ErrTooManyPartialSignatureMessages         = Error{text: "too many partial signature messages", reject: true}
	ErrEncodeOperators                         = Error{text: "encode operators", reject: true}
	ErrUnknownOperator                         = Error{text: "operator is unknown"}
	ErrOperatorValidation                      = Error{text: "failed to validate operator data"}
)

func (mv *messageValidator) handleValidationError(ctx context.Context, peerID peer.ID, decodedMessage *queue.SSVMessage, err error) pubsub.ValidationResult {
	loggerFields := mv.buildLoggerFields(decodedMessage)

	logger := mv.logger.
		With(loggerFields.AsZapFields()...).
		With(fields.PeerID(peerID))

	var valErr Error
	if !errors.As(err, &valErr) {
		recordIgnoredMessage(ctx, loggerFields.Role, err.Error())
		logger.Debug("ignoring invalid message", zap.Error(err))
		return pubsub.ValidationIgnore
	}

	if !valErr.Reject() {
		if !valErr.Silent() {
			logger.Debug("ignoring invalid message", zap.Error(valErr))
		}
		recordIgnoredMessage(ctx, loggerFields.Role, valErr.Text())
		return pubsub.ValidationIgnore
	}

	if !valErr.Silent() {
		logger.Debug("rejecting invalid message", zap.Error(valErr))
	}

	recordRejectedMessage(ctx, loggerFields.Role, valErr.Text())
	return pubsub.ValidationReject
}

func (mv *messageValidator) handleValidationSuccess(ctx context.Context, decodedMessage *queue.SSVMessage) pubsub.ValidationResult {
	recordAcceptedMessage(ctx, decodedMessage.GetID().GetRoleType())
	return pubsub.ValidationAccept
}
