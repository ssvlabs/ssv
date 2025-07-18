package observability

import (
	"github.com/ssvlabs/ssv/observability/log"
	"go.uber.org/zap"
)

var logger *zap.Logger

func initLogger(l *zap.Logger) *zap.Logger {
	if l == nil {
		l = zap.NewNop()
	}
	logger = l.Named(log.NameObservability)
	return logger
}
