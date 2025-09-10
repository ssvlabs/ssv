package observability

import (
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/observability/metrics"
	"github.com/ssvlabs/ssv/observability/traces"
)

func initLogger(l *zap.Logger) *zap.Logger {
	logger := l.Named(log.NameObservability)
	metrics.InitLogger(logger)
	traces.InitLogger(logger)

	return logger
}
