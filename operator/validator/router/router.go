package router

import (
	"context"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
)

const bufSize = 1024

func NewMessageRouter(logger *zap.Logger) *MessageRouter {
	return &MessageRouter{
		logger: logger,
		ch:     make(chan *queue.DecodedSSVMessage, bufSize),
	}
}

type MessageRouter struct {
	logger *zap.Logger
	ch     chan *queue.DecodedSSVMessage
}

func (r *MessageRouter) Route(ctx context.Context, message *queue.DecodedSSVMessage) {
	select {
	case <-ctx.Done():
		r.logger.Warn("context canceled, dropping message")
	case r.ch <- message:
	default:
		r.logger.Warn("message router buffer is full, dropping message")
	}
}

func (r *MessageRouter) Messages() <-chan *queue.DecodedSSVMessage {
	return r.ch
}
