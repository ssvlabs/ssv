package validator

import (
	"context"
	"sync/atomic"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network"
)

const bufSize = 65536

const bufferFillSampleRate = 64

func newMessageRouter(logger *zap.Logger) *messageRouter {
	return &messageRouter{
		logger: logger,
		ch:     make(chan network.DecodedSSVMessage, bufSize),
	}
}

type messageRouter struct {
	logger               *zap.Logger
	ch                   chan network.DecodedSSVMessage
	bufferFillSampleTick atomic.Uint64
}

func (r *messageRouter) Route(ctx context.Context, message network.DecodedSSVMessage) {
	r.route(ctx, message)
}

func (r *messageRouter) route(ctx context.Context, message network.DecodedSSVMessage) bool {
	select {
	case <-ctx.Done():
		recordRouterMessageDrop(context.Background(), routerDropReasonContextCanceled)
		r.logger.Debug("context canceled, dropping message")
		return false
	default:
	}

	select {
	case r.ch <- message:
		r.recordBufferFill(ctx)
		return true
	default:
		recordRouterMessageDrop(ctx, routerDropReasonBufferFull)
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
	if r.bufferFillSampleTick.Add(1)%bufferFillSampleRate != 0 {
		return
	}
	routerBufferFillGauge.Record(ctx, int64(len(r.ch)))
}
