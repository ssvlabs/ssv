package validation

import (
	"fmt"
)

// TODO: add rejection flag or create a separate type
// TODO: add inner error
type validationError struct {
	text string
	got  any
	want any
}

func (ve validationError) Error() string {
	result := ve.text
	if ve.got != nil {
		result += fmt.Sprintf(", got %v", ve.got)
	}
	if ve.want != nil {
		result += fmt.Sprintf(", want %v", ve.want)
	}

	return result
}

var (
	ErrEmptyData               = validationError{text: "empty data"}
	ErrDataTooBig              = validationError{text: "data too big", want: maxMessageSize}
	ErrUnknownValidator        = validationError{text: "unknown validator"}
	ErrInvalidRole             = validationError{text: "invalid role"}
	ErrEarlyMessage            = validationError{text: "early message"}
	ErrLateMessage             = validationError{text: "late message"}
	ErrNoSigners               = validationError{text: "no signers"}
	ErrZeroSignature           = validationError{text: "zero signature"}
	ErrZeroSigner              = validationError{text: "zero signer ID"}
	ErrSignerNotInCommittee    = validationError{text: "signer is not in committee"}
	ErrDuplicatedSigner        = validationError{text: "signer is duplicated"}
	ErrSignerNotLeader         = validationError{text: "signer is not leader"}
	ErrSignersNotSorted        = validationError{text: "signers are not sorted"}
	ErrUnexpectedSigner        = validationError{text: "signer is not expected"}
	ErrWrongDomain             = validationError{text: "wrong domain"}
	ErrValidatorLiquidated     = validationError{text: "validator is liquidated"}
	ErrValidatorNotAttesting   = validationError{text: "validator is not attesting"}
	ErrTooManyMessagesPerRound = validationError{text: "too many messages per round"}
	ErrUnexpectedMessageType   = validationError{text: "unexpected message type"}
	ErrSlotAlreadyAdvanced     = validationError{text: "signer has already advanced to a later slot"}
	ErrRoundAlreadyAdvanced    = validationError{text: "signer has already advanced to a later round"}
	ErrFutureSlotRoundMismatch = validationError{text: "if slot is in future, round must be also in future and vice versa"}
	ErrRoundTooFarInTheFuture  = validationError{text: "round is too far in the future"}
)
