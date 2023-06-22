package eventdispatcher

import (
	"go.uber.org/zap"
)

// Option defines EventDispatcher configuration option.
type Option func(*EventDispatcher)

// WithLogger enables logging.
func WithLogger(logger *zap.Logger) Option {
	return func(ed *EventDispatcher) {
		ed.logger = logger.Named("event_dispatcher")
	}
}

// WithMetrics enables reporting metrics.
func WithMetrics(metrics metrics) Option {
	return func(ed *EventDispatcher) {
		ed.metrics = metrics
	}
}

// WithNodeProber uses a node prober that checks node status.
func WithNodeProber(nodeProber nodeProber) Option {
	return func(ed *EventDispatcher) {
		ed.nodeProber = nodeProber
	}
}
