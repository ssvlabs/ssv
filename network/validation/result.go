package validation

import (
	"fmt"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/multierr"
)

type Reason string

const (
	ReasonEmptyData         Reason = "empty-data"
	ReasonMalformed         Reason = "malformed"
	ReasonValidatorNotFound Reason = "validator-not-found"
	ReasonNotTimely         Reason = "not-timely"
	ReasonSyntacticError    Reason = "syntactic-error"
	ReasonBetterMessage     Reason = "better-message"
	ReasonInvalidSig        Reason = "invalid-signature"
	ReasonTooManyMsgs       Reason = "too-many-msgs"
	ReasonError             Reason = "error"
)

// Result is a validation result which describes a problem with the message
// and whether it should be accepted, rejected, or ignored.
type Result struct {
	Action pubsub.ValidationResult
	Reason Reason
	Err    error
}

func (r Result) Error() string {
	if r.Err == nil {
		return fmt.Sprintf("rejected (%s)", r.Reason)
	}
	return fmt.Sprintf("rejected (%s): %s", r.Reason, r.Err)
}

func newResult(action pubsub.ValidationResult, reason Reason, errs ...error) error {
	return Result{
		Action: action,
		Reason: reason,
		Err:    multierr.Combine(errs...),
	}
}

func accept(reason Reason, errs ...error) error {
	return newResult(pubsub.ValidationAccept, reason, errs...)
}

func reject(reason Reason, errs ...error) error {
	return newResult(pubsub.ValidationReject, reason, errs...)
}

func ignore(reason Reason, errs ...error) error {
	return newResult(pubsub.ValidationIgnore, reason, errs...)
}
