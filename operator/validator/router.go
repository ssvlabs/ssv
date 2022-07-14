package validator

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"
)

const bufSize = 1024

func newMessageRouter(logger *zap.Logger) *messageRouter {
	return &messageRouter{
		logger: logger,
		ch:     make(chan spectypes.SSVMessage, bufSize),
	}
}

type messageRouter struct {
	logger *zap.Logger
	ch     chan spectypes.SSVMessage
}

func (r *messageRouter) Route(message spectypes.SSVMessage) {
	select {
	case r.ch <- message:
	default:
		// TODO(nkryuchkov): consider doing more than just logging
		r.logger.Warn("Message router buffer is full. Discarding message")
	}
}

func (r *messageRouter) GetMessageChan() <-chan spectypes.SSVMessage {
	return r.ch
}
