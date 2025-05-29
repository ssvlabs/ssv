package validator

import (
	"context"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network"
)

const bufSize = 65536

func newMessageRouter(logger *zap.Logger) *messageRouter {
	return &messageRouter{
		logger: logger,
		ch:     make(chan network.DecodedSSVMessage, bufSize),
	}
}

type messageRouter struct {
	logger *zap.Logger
	ch     chan network.DecodedSSVMessage
}

func (r *messageRouter) Route(ctx context.Context, message network.DecodedSSVMessage) {
	select {
	case <-ctx.Done():
		r.logger.Warn("context canceled, dropping message")
	case r.ch <- message:
	default:
		r.logger.Warn("message router buffer is full, dropping message")
	}
}

func (r *messageRouter) GetMessageChan() <-chan network.DecodedSSVMessage {
	return r.ch
}
