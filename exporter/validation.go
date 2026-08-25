package exporter

import (
	"fmt"
	"math"
)

// validateSlotRange checks the inclusive [from, to] slot window shared by
// every exporter range endpoint. The per-slot loops are inclusive of 'to',
// so the maximum uint64 value would wrap the counter and never terminate.
// An endpoint-wide range-size bound is tracked in #2986; this only rejects
// an inverted window and the guaranteed hang.
func validateSlotRange(from, to uint64) error {
	if from > to {
		return fmt.Errorf("'from' must be less than or equal to 'to'")
	}
	if to == math.MaxUint64 {
		return fmt.Errorf("'to' must be less than %d", uint64(math.MaxUint64))
	}
	return nil
}

// ValidationError wraps an underlying error to indicate that a request is semantically invalid.
// It allows callers to distinguish validation errors from processing errors using errors.As.
type ValidationError struct {
	Err error
}

func (e *ValidationError) Error() string {
	if e == nil || e.Err == nil {
		return "validation error"
	}
	return e.Err.Error()
}

func (e *ValidationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
