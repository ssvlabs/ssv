package domain

import (
	"context"
	"io"
)

// NoopRegistrationForwarder always reports success.
type NoopRegistrationForwarder struct{}

func (n NoopRegistrationForwarder) ForwardValidatorRegistrations(context.Context, io.ReadCloser) ([]string, error) {
	return nil, nil
}
