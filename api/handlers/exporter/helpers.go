package exporter

import (
	"errors"

	"github.com/ssvlabs/ssv/exporter2"
)

func isValidationError(err error) bool {
	if err == nil {
		return false
	}
	var vErr *exporter2.ValidationError
	return errors.As(err, &vErr)
}

func underlyingValidationError(err error) error {
	if err == nil {
		return nil
	}
	var vErr *exporter2.ValidationError
	if errors.As(err, &vErr) {
		if vErr.Err != nil {
			return vErr.Err
		}
		return vErr
	}
	return err
}
