package eventdatahandler

import (
	"github.com/bloxapp/ssv/logging"
	"go.uber.org/zap"
)

// Option defines EventDataHandler configuration option.
type Option func(*EventDataHandler)

// WithLogger enables logging.
func WithLogger(logger *zap.Logger) Option {
	return func(edh *EventDataHandler) {
		edh.logger = logger.Named(logging.NameEventHandler)
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
