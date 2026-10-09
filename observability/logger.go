package observability

import (
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/v2/observability/log"
	"github.com/ssvlabs/ssv/v2/observability/metrics"
	"github.com/ssvlabs/ssv/v2/observability/traces"
)

func initLogger(l *zap.Logger) *zap.Logger {
	logger := l.Named(log.NameObservability)
	metrics.InitLogger(logger)
	traces.InitLogger(logger)

	return logger
}
