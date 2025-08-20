package eventsyncer

import (
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
