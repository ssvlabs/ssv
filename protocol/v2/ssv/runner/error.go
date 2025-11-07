package runner

import (
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/trace"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/traces"
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
	// ErrNoDecidedValue means we might not have finished the QBFT consensus phase yet, while another operator
	// already did + sent this message to us.
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

// Errorf sets the status of the span to error and returns an error with the formatted message.
func tracedErrorf(span trace.Span, f string, args ...any) error {
	err := fmt.Errorf(f, args...)
	return tracedError(span, err)
}

func tracedError(span trace.Span, err error) error {
	// In case of retryable error, mark this span's subtree as "droppable" since we don't necessarily want
	// to keep track of those retry-related spans (or their child-spans) - these generate lots of useless data.
	if IsRetryable(err) {
		observability.SpanProcessor.MarkDropSubtree(span.SpanContext())
	}

	return traces.Error(span, err)
}
