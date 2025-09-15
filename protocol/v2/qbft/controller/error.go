package controller

import (
	"errors"
	"fmt"
)

var (
	// ErrFutureConsensusMsg means the message we've got is "from the future"; it can happen if we haven't started
	// QBFT instance for the slot the message is targeting yet, while another operator already did + sent this
	// message to us.
	ErrFutureConsensusMsg = fmt.Errorf("future consensus msg")
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
