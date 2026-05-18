package validation

import (
	"go.uber.org/zap"
)

// Option represents a functional option for configuring a messageValidator.
type Option func(validator *messageValidator)

// WithLogger sets the logger for the messageValidator.
func WithLogger(logger *zap.Logger) Option {
	return func(mv *messageValidator) {
		mv.logger = logger
	}
}

// WithSSVValidationObserver sets an observer for SSV-level validation decisions.
func WithSSVValidationObserver(observer SSVValidationObserver) Option {
	return func(mv *messageValidator) {
		mv.observer = observer
	}
}
