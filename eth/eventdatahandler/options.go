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

// WithFullNode signals that node works in a full node state.
func WithFullNode() Option {
	return func(edh *EventDataHandler) {
		edh.fullNode = true
	}
}

// WithTaskOptimizer enables task optimization.
// NOTE: EXPERIMENTAL, not for production use
// TODO: check if start-stop can be superseded by reactivate-liquidate
func WithTaskOptimizer() Option {
	return func(edh *EventDataHandler) {
		edh.taskOptimization = true
	}
}
