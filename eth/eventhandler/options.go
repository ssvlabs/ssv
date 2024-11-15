package eventhandler

import (
	"github.com/ssvlabs/ssv/logging"
	"go.uber.org/zap"
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
