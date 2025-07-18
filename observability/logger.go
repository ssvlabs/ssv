package observability

import (
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/observability/metrics"
	"go.uber.org/zap"
)

var logger *zap.Logger

func initLogger(l *zap.Logger) *zap.Logger {
	if l == nil {
		l = zap.NewNop()
	}

	logger = l.Named(log.NameObservability)
	metrics.InitLogger(logger)

	return logger
}
