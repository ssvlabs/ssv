package validation

import "errors"

type validationStageError struct {
	stage string
	err   error
}

func (e validationStageError) Error() string {
	return e.err.Error()
}

func (e validationStageError) Unwrap() error {
	return e.err
}

func withValidationStage(stage string, err error) error {
	if err == nil {
		return nil
	}
	if validationStageFromError(err) != SSVValidationStageUnknown {
		return err
	}
	return validationStageError{stage: stage, err: err}
}

func validationStageFromError(err error) string {
	var staged validationStageError
	if errors.As(err, &staged) {
		return staged.stage
	}
	return SSVValidationStageUnknown
}
