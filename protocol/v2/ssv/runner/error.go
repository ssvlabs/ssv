package runner

import (
	"errors"
	"fmt"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var (
	// ErrNoValidDutiesToExecute means committee runner has no duties to execute (even though the committee runner
	// had to do some work to arrive at that conclusion)
	ErrNoValidDutiesToExecute = fmt.Errorf("committee has no valid duties to execute")
	// ErrDutyInvariantViolation marks a terminal that reached a state the code's own invariants say is
	// unreachable. It is wrapped alongside ErrNoValidDutiesToExecute where the queue must still drop the
	// message and terminate the runner, but the terminal is a correctness signal rather than a benign
	// no-op — so it stays loud (duty concluded failed, trace span marked as an error) instead of being
	// classified with the benign zero-duties terminals.
	ErrDutyInvariantViolation = fmt.Errorf("duty invariant violation")
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

// codedError pairs an error with the spec error code it reports. spectypes.WrapError alone would hide the
// error: the spec's Error type has no Unwrap, so a sentinel it wraps is invisible to errors.Is, and a tag
// such as recoverableReconstructError to errors.As. Unwrap exposes both — the coded error for errors.As
// (spec tests, observability) and the error itself — under the error's own message text.
type codedError struct {
	coded *spectypes.Error
	err   error
}

// withCode tags err with a spec error code; see codedError.
func withCode(code int, err error) error {
	return &codedError{coded: spectypes.WrapError(code, err), err: err}
}

func (e *codedError) Error() string { return e.err.Error() }

func (e *codedError) Unwrap() []error { return []error{e.coded, e.err} }

// recoverableReconstructError tags a BLS-reconstruction failure as recoverable: FallBackAndVerifyEachSignature
// has dropped the offending partial sig(s), so a later partial-sig message re-crosses quorum and retries. It is
// attached by reconstructQuorumSig, only when the drop left the root below quorum. Classifying by this tag
// (rather than by the spec ReconstructSignatureErrorCode) covers the whole recoverable subclass:
// VerifyReconstructedSignature attaches the code, but the earlier BLS Deserialize/Recover step (e.g. 96 garbage
// bytes from a byzantine operator) returns an uncoded error that is equally recoverable. Errors without this tag
// stay terminal. Unwrap keeps the wrapped chain (including any code-tagged *spectypes.Error) reachable via
// errors.As, so the reconstruct error code is still observable.
type recoverableReconstructError struct {
	err error
}

func (e recoverableReconstructError) Error() string { return e.err.Error() }

func (e recoverableReconstructError) Unwrap() error { return e.err }

func isRecoverableReconstructError(err error) bool {
	var rec recoverableReconstructError
	return errors.As(err, &rec)
}
