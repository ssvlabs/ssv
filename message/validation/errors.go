package validation

import (
	"fmt"
)

type Error struct {
	text     string
	got      any
	want     any
	innerErr error
	reject   bool
}

func (e Error) Error() string {
	result := e.text
	if e.got != nil {
		result += fmt.Sprintf(", got %v", e.got)
	}
	if e.want != nil {
		result += fmt.Sprintf(", want %v", e.want)
	}
	if e.innerErr != nil {
		result += fmt.Sprintf(": %s", e.innerErr.Error())
	}

	return result
}

func (e Error) Reject() bool {
	return e.reject
}

func (e Error) Text() string {
	return e.text
}

var (
	ErrEmptyData                           = Error{text: "empty data"}
	ErrWrongDomain                         = Error{text: "wrong domain"}
	ErrUnknownValidator                    = Error{text: "unknown validator"}
	ErrValidatorLiquidated                 = Error{text: "validator is liquidated"}
	ErrValidatorNotAttesting               = Error{text: "validator is not attesting"}
	ErrSlotAlreadyAdvanced                 = Error{text: "signer has already advanced to a later slot"}
	ErrRoundAlreadyAdvanced                = Error{text: "signer has already advanced to a later round"}
	ErrFutureSlotRoundMismatch             = Error{text: "if slot is in future, round must be also in future and vice versa"}
	ErrRoundTooFarInTheFuture              = Error{text: "round is too far in the future"}
	ErrRoundTooHigh                        = Error{text: "round is too high for this role" /*, reject: true*/} // TODO: enable reject
	ErrEarlyMessage                        = Error{text: "early message"}
	ErrLateMessage                         = Error{text: "late message"}
	ErrDataTooBig                          = Error{text: "data too big", reject: true}
	ErrInvalidRole                         = Error{text: "invalid role", reject: true}
	ErrNoSigners                           = Error{text: "no signers", reject: true}
	ErrWrongSignatureSize                  = Error{text: "wrong signature size", reject: true}
	ErrZeroSignature                       = Error{text: "zero signature", reject: true}
	ErrZeroSigner                          = Error{text: "zero signer ID", reject: true}
	ErrSignerNotInCommittee                = Error{text: "signer is not in committee", reject: true}
	ErrDuplicatedSigner                    = Error{text: "signer is duplicated", reject: true}
	ErrSignerNotLeader                     = Error{text: "signer is not leader", reject: true}
	ErrSignersNotSorted                    = Error{text: "signers are not sorted", reject: true}
	ErrUnexpectedSigner                    = Error{text: "signer is not expected", reject: true}
	ErrTooManyMessagesPerRound             = Error{text: "too many messages per round"}
	ErrUnexpectedMessageType               = Error{text: "unexpected message type", reject: true}
	ErrInvalidHash                         = Error{text: "root doesn't match full data hash", reject: true}
	ErrInvalidSignature                    = Error{text: "invalid signature", reject: true}
	ErrEstimatedRoundTooFar                = Error{text: "message round is too far from estimated"}
	ErrMalformedMessage                    = Error{text: "message could not be decoded", reject: true}
	ErrUnknownMessageType                  = Error{text: "unknown message type", reject: true}
	ErrPartialSignatureTypeRoleMismatch    = Error{text: "partial signature type and role don't match", reject: true}
	ErrNonDecidedWithMultipleSigners       = Error{text: "non-decided with multiple signers", reject: true}
	ErrWrongSignersLength                  = Error{text: "decided signers size is not between quorum and committee size", reject: true}
	ErrDuplicatedProposalWithDifferentData = Error{text: "duplicated proposal with different data", reject: true}
	ErrDecidedSignersSequence              = Error{text: "decided must have more signers than previous decided", reject: true}
	ErrEventMessage                        = Error{text: "event messages are not broadcast", reject: true}
)
