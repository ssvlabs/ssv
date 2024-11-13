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
		ch:     make(chan VMSG, bufSize),
	}
}

type messageRouter struct {
	logger *zap.Logger
	ch     chan VMSG
}

type VMSG struct {
	network.DecodedSSVMessage
	ctx context.Context
}

func (r *messageRouter) Route(ctx context.Context, message network.DecodedSSVMessage) {
	select {
	case <-ctx.Done():
		r.logger.Warn("context canceled, dropping message")
	case r.ch <- VMSG{ctx: ctx, DecodedSSVMessage: message}:
	default:
		r.logger.Warn("message router buffer is full, dropping message")
	}
}

func (r *messageRouter) GetMessageChan() <-chan VMSG {
	return r.ch
}
