package eventbatcher

import (
	"go.uber.org/zap"
)

// Option defines EventDataHandler configuration option.
type Option func(*EventBatcher)

// WithLogger enables logging.
func WithLogger(logger *zap.Logger) Option {
	return func(edh *EventBatcher) {
		edh.logger = logger.Named("event_batcher")
	}
}
