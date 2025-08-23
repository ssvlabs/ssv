package runner

import (
	"errors"
	"fmt"
)

var (
	// ErrNoValidDutiesToExecute means committee runner has no duties to execute (even though the committee runner
	// had to do some work to arrive at that conclusion)
	ErrNoValidDutiesToExecute = fmt.Errorf("committee has no valid duties to execute")
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

// RetryableError is an error-wrapper to indicate that wrapped error is retryable.
type RetryableError struct {
	originalErr error
}

func NewRetryableError(originalErr error) *RetryableError {
	return &RetryableError{
		originalErr: originalErr,
	}
}

func (e RetryableError) Error() string {
	return e.originalErr.Error()
}

func (e RetryableError) Is(target error) bool {
	var retryableErr *RetryableError
	ok := errors.As(target, &retryableErr)
	return ok
}
