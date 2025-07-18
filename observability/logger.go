package observability

import (
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/observability/metrics"
	"github.com/ssvlabs/ssv/observability/traces"
	"go.uber.org/zap"
)

var logger *zap.Logger

func initLogger(l *zap.Logger) *zap.Logger {
	if l == nil {
		l = zap.NewNop()
	}

	logger = l.Named(log.NameObservability)
	metrics.InitLogger(logger)
	traces.InitLogger(logger)

	return logger
}
