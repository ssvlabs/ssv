package exporter2

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
