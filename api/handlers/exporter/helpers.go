package exporter

import (
	"errors"

	exportercore "github.com/ssvlabs/ssv/exporter"
)

func isValidationError(err error) bool {
	if err == nil {
		return false
	}
	var vErr *exportercore.ValidationError
	return errors.As(err, &vErr)
}

func underlyingValidationError(err error) error {
	if err == nil {
		return nil
	}
	var vErr *exportercore.ValidationError
	if errors.As(err, &vErr) {
		if vErr.Err != nil {
			return vErr.Err
		}
		return vErr
	}
	return err
}
