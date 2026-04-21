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

func (r *messageRouter) Route(ctx context.Context, message network.DecodedSSVMessage) bool {
	select {
	case <-ctx.Done():
		r.logger.Warn("context canceled, dropping message")
		return false
	case r.ch <- message:
		r.recordBufferFill(ctx)
		return true
	default:
		routerDroppedCounter.Add(ctx, 1)
		r.recordBufferFill(ctx)
		r.logger.Warn("message router buffer is full, dropping message")
		return false
	}
}

func (r *messageRouter) Receive(ctx context.Context) (network.DecodedSSVMessage, bool) {
	select {
	case <-ctx.Done():
		return nil, false
	case msg := <-r.ch:
		r.recordBufferFill(ctx)
		return msg, true
	}
}

func (r *messageRouter) Len() int {
	return len(r.ch)
}

func (r *messageRouter) recordBufferFill(ctx context.Context) {
	routerBufferFillGauge.Record(ctx, int64(len(r.ch)))
}
