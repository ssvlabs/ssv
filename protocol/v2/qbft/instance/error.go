package instance

import (
	"errors"
	"fmt"
)

var (
	// ErrWrongMsgRound means we might not have changed the round to the next one yet, while another operator
	// already did + sent this message to us.
	ErrWrongMsgRound = fmt.Errorf("wrong msg round")
	// ErrNoProposalForCurrentRound means we might not have received a proposal-message yet, while another operator already
	// did and started preparing it.
	ErrNoProposalForCurrentRound = fmt.Errorf("did not receive proposal for current round")

	// ErrWrongMsgHeight means our QBFT instance expects different height. It is a sanity-check we have to do
	// for our implementation to work properly (to filter out irrelevant messages).
	ErrWrongMsgHeight = fmt.Errorf("wrong msg height")
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
