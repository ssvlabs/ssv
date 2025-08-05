package eventsyncer

import (
	"time"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log"
)

// Option defines EventSyncer configuration option.
type Option func(*EventSyncer)

// WithLogger enables logging.
func WithLogger(logger *zap.Logger) Option {
	return func(es *EventSyncer) {
		es.logger = logger.Named(log.NameEventSyncer)
	}
}

func WithStalenessThreshold(threshold time.Duration) Option {
	return func(es *EventSyncer) {
		es.stalenessThreshold = threshold
	}
}
