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
	// ErrRunningDutySucceeded means we have successfully finished the duty already, while another operator hasn't
	// finished it yet + sent this message to us.
	ErrRunningDutySucceeded = fmt.Errorf("running duty already succeeded")
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

// recoverableReconstructError tags a post-consensus BLS-reconstruction failure as recoverable.
// It is attached at the push site inside the reconstruct goroutine, which has by construction
// already run FallBackAndVerifyEachSignature to drop the offending partial sig(s) — so a later
// partial-sig message can re-cross quorum and retry the pending roots. Classifying by this tag
// (rather than by the spec ReconstructSignatureErrorCode) covers the whole recoverable subclass:
// VerifyReconstructedSignature attaches the code, but the earlier BLS Deserialize/Recover step
// (e.g. 96 garbage bytes from a byzantine operator) returns an uncoded error that is equally
// recoverable. Errors reaching the classifier without this tag stay terminal by default.
// Unwrap keeps the wrapped chain (including any code-tagged *spectypes.Error) reachable via
// errors.As so the committee role still observably emits the reconstruct error code.
type recoverableReconstructError struct {
	err error
}

func (e recoverableReconstructError) Error() string { return e.err.Error() }

func (e recoverableReconstructError) Unwrap() error { return e.err }

func isRecoverableReconstructError(err error) bool {
	var rec recoverableReconstructError
	return errors.As(err, &rec)
}
