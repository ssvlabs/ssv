package observability

import (
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
)

var logger *zap.Logger

func initLogger(l *zap.Logger) *zap.Logger {
	if l == nil {
		l = zap.NewNop()
	}
	logger = l.Named(logging.NameObservability)
	return logger
}
