package eventdatahandler

import (
	"go.uber.org/zap"
)

// Option defines EventDataHandler configuration option.
type Option func(*EventDataHandler)

// WithLogger enables logging.
func WithLogger(logger *zap.Logger) Option {
	return func(edh *EventDataHandler) {
		edh.logger = logger.Named("event_data_handler")
	}
}

// WithMetrics enables reporting metrics.
func WithMetrics(metrics metrics) Option {
	return func(edh *EventDataHandler) {
		edh.metrics = metrics
	}
}
