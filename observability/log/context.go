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
