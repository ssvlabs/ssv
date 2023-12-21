package metricsreporter

import (
	"go.uber.org/zap"
)

// Option defines EventSyncer configuration option.
type Option func(reporter *metricsReporter)

// WithLogger enables logging.
func WithLogger(logger *zap.Logger) Option {
	return func(ed *metricsReporter) {
		ed.logger = logger.Named("metrics_reporter")
	}
}
