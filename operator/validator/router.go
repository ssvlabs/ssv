package validator

import (
	"context"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
)

const bufSize = 1024

func newMessageRouter(logger *zap.Logger) *messageRouter {
	return &messageRouter{
		logger: logger,
		ch:     make(chan *queue.DecodedSSVMessage, bufSize),
	}
}

type messageRouter struct {
	logger *zap.Logger
	ch     chan *queue.DecodedSSVMessage
}

func (r *messageRouter) Route(ctx context.Context, message *queue.DecodedSSVMessage) {
	select {
	case <-ctx.Done():
		r.logger.Warn("context canceled, dropping message")
	case r.ch <- message:
	default:
		r.logger.Warn("message router buffer is full, dropping message")
	}
}

func (r *messageRouter) GetMessageChan() <-chan *queue.DecodedSSVMessage {
	return r.ch
}
