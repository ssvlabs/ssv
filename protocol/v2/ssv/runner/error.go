package runner

import (
	"fmt"
)

var (
	ErrNoValidDutiesToExecute = fmt.Errorf("no valid duties to execute")

	// Below is a list of retryable errors a runner might encounter.

	// ErrNoRunningDuty means we might not have started the duty yet, while another operator already did + sent this
	// message to us.
	ErrNoRunningDuty = fmt.Errorf("no running duty")
	// ErrFuturePartialSigMsg means the message we've got is "from the future"; it can happen if we haven't advanced
	// the runner to the slot the message is targeting yet, while another operator already did + sent this message
	// to us.
	ErrFuturePartialSigMsg = fmt.Errorf("future partial sig msg")
	// ErrInstanceNotFound means we might not have started the QBFT instance yet, while another operator already did
	// + sent this message to us.
	ErrInstanceNotFound = fmt.Errorf("instance not found")
)

type RetryableError struct {
	originalErr error
}

func NewRetryableError(originalErr error) *RetryableError {
	return &RetryableError{
		originalErr: originalErr,
	}
}

func (e *RetryableError) Error() string {
	return e.originalErr.Error()
}
