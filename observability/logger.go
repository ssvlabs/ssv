package observability

import "go.uber.org/zap"

var logger *zap.Logger

func initLogger(l *zap.Logger) *zap.Logger {
	logger = l.Named("Observability")
	return logger
}
