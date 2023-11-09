package validation

import (
	"fmt"
	"strings"
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
	ErrEmptyData                           = Error{text: "empty data"}
	ErrWrongDomain                         = Error{text: "wrong domain", silent: true}
	ErrNoShareMetadata                     = Error{text: "share has no metadata"}
	ErrUnknownValidator                    = Error{text: "unknown validator"}
	ErrValidatorLiquidated                 = Error{text: "validator is liquidated"}
	ErrValidatorNotAttesting               = Error{text: "validator is not attesting"}
	ErrSlotAlreadyAdvanced                 = Error{text: "signer has already advanced to a later slot"}
	ErrRoundAlreadyAdvanced                = Error{text: "signer has already advanced to a later round"}
	ErrRoundTooHigh                        = Error{text: "round is too high for this role" /*, reject: true*/} // TODO: enable reject
	ErrEarlyMessage                        = Error{text: "early message"}
	ErrLateMessage                         = Error{text: "late message"}
	ErrTooManySameTypeMessagesPerRound     = Error{text: "too many messages of same type per round"}
	ErrRSADecryption                       = Error{text: "rsa decryption", reject: true}
	ErrOperatorNotFound                    = Error{text: "operator not found", reject: true}
	ErrPubSubMessageHasNoData              = Error{text: "pub-sub message has no data", reject: true}
	ErrPubSubDataTooBig                    = Error{text: "pub-sub message data too big", reject: true}
	ErrMalformedPubSubMessage              = Error{text: "pub-sub message is malformed", reject: true}
	ErrEmptyPubSubMessage                  = Error{text: "pub-sub message is empty", reject: true}
	ErrTopicNotFound                       = Error{text: "topic not found", reject: true}
	ErrSSVDataTooBig                       = Error{text: "ssv message data too big", reject: true}
	ErrInvalidRole                         = Error{text: "invalid role", reject: true}
	ErrConsensusValidatorRegistration      = Error{text: "consensus message for validator registration role", reject: true}
	ErrNoSigners                           = Error{text: "no signers", reject: true}
	ErrWrongSignatureSize                  = Error{text: "wrong signature size", reject: true}
	ErrZeroSignature                       = Error{text: "zero signature", reject: true}
	ErrZeroSigner                          = Error{text: "zero signer ID", reject: true}
	ErrSignerNotInCommittee                = Error{text: "signer is not in committee", reject: true}
	ErrDuplicatedSigner                    = Error{text: "signer is duplicated", reject: true}
	ErrSignerNotLeader                     = Error{text: "signer is not leader", reject: true}
	ErrSignersNotSorted                    = Error{text: "signers are not sorted", reject: true}
	ErrUnexpectedSigner                    = Error{text: "signer is not expected", reject: true}
	ErrInvalidHash                         = Error{text: "root doesn't match full data hash", reject: true}
	ErrEstimatedRoundTooFar                = Error{text: "message round is too far from estimated"}
	ErrMalformedMessage                    = Error{text: "message could not be decoded", reject: true}
	ErrMalformedSignedMessage              = Error{text: "signed message could not be decoded", reject: true}
	ErrUnknownSSVMessageType               = Error{text: "unknown SSV message type", reject: true}
	ErrUnknownQBFTMessageType              = Error{text: "unknown QBFT message type", reject: true}
	ErrUnknownPartialMessageType           = Error{text: "unknown partial signature message type", reject: true}
	ErrPartialSignatureTypeRoleMismatch    = Error{text: "partial signature type and role don't match", reject: true}
	ErrNonDecidedWithMultipleSigners       = Error{text: "non-decided with multiple signers", reject: true}
	ErrWrongSignersLength                  = Error{text: "decided signers size is not between quorum and committee size", reject: true}
	ErrDuplicatedProposalWithDifferentData = Error{text: "duplicated proposal with different data", reject: true}
	ErrEventMessage                        = Error{text: "event messages are not broadcast", reject: true}
	ErrDKGMessage                          = Error{text: "DKG messages are not supported", reject: true}
	ErrMalformedPrepareJustifications      = Error{text: "malformed prepare justifications", reject: true}
	ErrUnexpectedPrepareJustifications     = Error{text: "prepare justifications unexpected for this message type", reject: true}
	ErrMalformedRoundChangeJustifications  = Error{text: "malformed round change justifications", reject: true}
	ErrUnexpectedRoundChangeJustifications = Error{text: "round change justifications unexpected for this message type", reject: true}
	ErrInvalidJustifications               = Error{text: "invalid justifications", reject: true}
	ErrTooManyDutiesPerEpoch               = Error{text: "too many duties per epoch", reject: true}
	ErrNoDuty                              = Error{text: "no duty for this epoch", reject: true}
	ErrDeserializePublicKey                = Error{text: "deserialize public key", reject: true}
	ErrNoPartialMessages                   = Error{text: "no partial messages", reject: true}
	ErrDuplicatedPartialSignatureMessage   = Error{text: "duplicated partial signature message", reject: true}
)
