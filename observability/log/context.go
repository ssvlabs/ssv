package log

import (
	"context"

	"go.uber.org/zap"
)

type loggerKeyType string

var loggerKey loggerKeyType = "LOGGER_KEY"

// FromContext returns a logger from the context.
func FromContext(ctx context.Context) *zap.Logger {
	logger, ok := ctx.Value(loggerKey).(*zap.Logger)
	if !ok {
		return zap.L()
	}
	return logger
}

// WithContext returns a context with the logger.
func WithContext(ctx context.Context, logger *zap.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}
