package eventhandler

import (
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
)

// Option defines EventHandler configuration option.
type Option func(*EventHandler)

// WithLogger enables logging.
func WithLogger(logger *zap.Logger) Option {
	return func(eh *EventHandler) {
		eh.logger = logger.Named(logging.NameEventHandler)
	}
}

// WithFullNode signals that node works in a full node state.
func WithFullNode() Option {
	return func(eh *EventHandler) {
		eh.fullNode = true
	}
}
