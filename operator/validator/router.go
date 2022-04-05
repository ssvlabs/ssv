package validator

import (
	"github.com/bloxapp/ssv/protocol/v1/message"
	"go.uber.org/zap"
)

const bufSize = 1024

func newMessageRouter(logger *zap.Logger) *messageRouter {
	return &messageRouter{
		logger: logger,
		ch:     make(chan message.SSVMessage, bufSize),
	}
}

type messageRouter struct {
	logger *zap.Logger
	ch     chan message.SSVMessage
}

func (r *messageRouter) Route(message message.SSVMessage) {
	select {
	case r.ch <- message:
	default:
		// TODO(nkryuchkov): consider doing more than just logging
		r.logger.Warn("Message router buffer is full. Discarding message")
	}
}

func (r *messageRouter) GetMessageChan() <-chan message.SSVMessage {
	return r.ch
}
