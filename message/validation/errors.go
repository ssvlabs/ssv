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

// TODO: add reject where needed
var (
	ErrEmptyData               = Error{text: "empty data"}
	ErrDataTooBig              = Error{text: "data too big", want: maxMessageSize}
	ErrUnknownValidator        = Error{text: "unknown validator"}
	ErrInvalidRole             = Error{text: "invalid role"}
	ErrEarlyMessage            = Error{text: "early message"}
	ErrLateMessage             = Error{text: "late message"}
	ErrNoSigners               = Error{text: "no signers"}
	ErrZeroSignature           = Error{text: "zero signature"}
	ErrZeroSigner              = Error{text: "zero signer ID"}
	ErrSignerNotInCommittee    = Error{text: "signer is not in committee"}
	ErrDuplicatedSigner        = Error{text: "signer is duplicated"}
	ErrSignerNotLeader         = Error{text: "signer is not leader", reject: true}
	ErrSignersNotSorted        = Error{text: "signers are not sorted"}
	ErrUnexpectedSigner        = Error{text: "signer is not expected"}
	ErrWrongDomain             = Error{text: "wrong domain"}
	ErrValidatorLiquidated     = Error{text: "validator is liquidated"}
	ErrValidatorNotAttesting   = Error{text: "validator is not attesting"}
	ErrTooManyMessagesPerRound = Error{text: "too many messages per round"}
	ErrUnexpectedMessageType   = Error{text: "unexpected message type"}
	ErrSlotAlreadyAdvanced     = Error{text: "signer has already advanced to a later slot"}
	ErrRoundAlreadyAdvanced    = Error{text: "signer has already advanced to a later round"}
	ErrFutureSlotRoundMismatch = Error{text: "if slot is in future, round must be also in future and vice versa"}
	ErrRoundTooFarInTheFuture  = Error{text: "round is too far in the future"}
	ErrRoundTooHigh            = Error{text: "round is too high for this role"}
)
