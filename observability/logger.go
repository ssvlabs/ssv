package observability

import "go.uber.org/zap"

var logger *zap.Logger

func initLogger(l *zap.Logger) *zap.Logger {
	if l == nil {
		l = zap.NewNop()
	}
	logger = l.Named("Observability")
	return logger
}
