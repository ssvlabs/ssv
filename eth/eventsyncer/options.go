package eventsyncer

import (
	"time"

	"go.uber.org/zap"
)

// Option defines EventSyncer configuration option.
type Option func(*EventSyncer)

// WithLogger enables logging.
func WithLogger(logger *zap.Logger) Option {
	return func(es *EventSyncer) {
		es.logger = logger.Named("EventSyncer")
	}
}

func WithStalenessThreshold(threshold time.Duration) Option {
	return func(es *EventSyncer) {
		es.stalenessThreshold = threshold
	}
}
