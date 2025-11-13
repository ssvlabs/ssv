package runner

import (
	"errors"
	"fmt"
)

var (
	// ErrNoValidDutiesToExecute means committee runner has no duties to execute (even though the committee runner
	// had to do some work to arrive at that conclusion)
	ErrNoValidDutiesToExecute = fmt.Errorf("committee has no valid duties to execute")
	// ErrNoDutyAssigned means we haven't started the duty yet, while another operator already has + sent
	// this message to us.
	ErrNoDutyAssigned = fmt.Errorf("no duty assigned")
	// ErrRunningDutyFinished means we have finished the duty already, while another operator hasn't finished it
	// yet + sent this message to us.
	ErrRunningDutyFinished = fmt.Errorf("running duty finished")
	// ErrFuturePartialSigMsg means the message we've got is "from the future"; it can happen if we haven't advanced
	// the runner to the slot the message is targeting yet, while another operator already has + sent this message
	// to us.
	ErrFuturePartialSigMsg = fmt.Errorf("future partial sig msg")
	// ErrInstanceNotFound means we might not have started the QBFT instance yet, while another operator already has
	// + sent this message to us.
	ErrInstanceNotFound = fmt.Errorf("instance not found")
	// ErrNoDecidedValue means we might not have finished the QBFT consensus phase yet, while another operator
	// already has + sent this message to us.
	ErrNoDecidedValue = fmt.Errorf("no decided value")
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

func (e RetryableError) Unwrap() error {
	return e.originalErr
}

func IsRetryable(err error) bool {
	var retryableErr *RetryableError
	return errors.As(err, &retryableErr)
}
