package metricsreporter

import (
	"go.uber.org/zap"
)

// Option defines EventSyncer configuration option.
type Option func(reporter *MetricsReporter)

// WithLogger enables logging.
func WithLogger(logger *zap.Logger) Option {
	return func(ed *MetricsReporter) {
		ed.logger = logger.Named("metrics_reporter")
	}
}
