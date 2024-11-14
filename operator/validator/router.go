package validator

import (
	"context"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network"
)

const bufSize = 65536

func newMessageRouter(ctx context.Context, logger *zap.Logger) *messageRouter {
	return &messageRouter{
		ctx:    ctx,
		logger: logger,
		ch:     make(chan VMSG, bufSize),
	}
}

type messageRouter struct {
	ctx    context.Context
	logger *zap.Logger
	ch     chan VMSG
}

type VMSG struct {
	network.DecodedSSVMessage
	ctx context.Context
}

func (r *messageRouter) Route(message network.DecodedSSVMessage) {
	ctx := r.ctx

	// TODO create new (tracing) context from request data:
	// ctx := otel.GetTextMapPropagator().Extract(ctx, message.TraceData)
	// pass the context down via r.ch

	select {
	case <-r.ctx.Done():
		r.logger.Warn("context canceled, dropping message")
	case r.ch <- VMSG{ctx: ctx, DecodedSSVMessage: message}:
	default:
		r.logger.Warn("message router buffer is full, dropping message")
	}
}

func (r *messageRouter) GetMessageChan() <-chan VMSG {
	return r.ch
}
