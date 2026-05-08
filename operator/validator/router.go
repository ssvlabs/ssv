package validator

import (
	"context"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/v2/network"
)

const bufSize = 65536

const bufferFillSampleRate = 64

func newMessageRouter(logger *zap.Logger) *messageRouter {
	return newMessageRouterWithMetrics(logger, routerDroppedMessagesCounter, routerBufferFillGauge)
}

// newMessageRouterWithMetrics builds a router with injected metrics — used by tests
// to assert on counter/gauge values without depending on global OTel state.
func newMessageRouterWithMetrics(logger *zap.Logger, droppedCounter metric.Int64Counter, bufferFillGauge metric.Int64Gauge) *messageRouter {
	return &messageRouter{
		logger:             logger,
		ch:                 make(chan network.DecodedSSVMessage, bufSize),
		droppedCounter:     droppedCounter,
		bufferFillGauge:    bufferFillGauge,
		dropAddOpsByReason: routerDropAddOpsByReason(),
	}
}

// routerDropAddOpsByReason builds the pre-allocated AddOptions for each known
// drop reason so recordDrop doesn't allocate attributes on the hot path.
func routerDropAddOpsByReason() map[string][]metric.AddOption {
	return map[string][]metric.AddOption{
		routerDropReasonContextCanceled: {
			metric.WithAttributeSet(attribute.NewSet(
				attribute.String("ssv.validator.router.drop_reason", routerDropReasonContextCanceled),
			)),
		},
		routerDropReasonBufferFull: {
			metric.WithAttributeSet(attribute.NewSet(
				attribute.String("ssv.validator.router.drop_reason", routerDropReasonBufferFull),
			)),
		},
	}
}

type messageRouter struct {
	logger               *zap.Logger
	ch                   chan network.DecodedSSVMessage
	bufferFillSampleTick atomic.Uint64
	droppedCounter       metric.Int64Counter
	bufferFillGauge      metric.Int64Gauge
	dropAddOpsByReason   map[string][]metric.AddOption
}

func (r *messageRouter) Route(ctx context.Context, message network.DecodedSSVMessage) {
	r.route(ctx, message)
}

func (r *messageRouter) route(ctx context.Context, message network.DecodedSSVMessage) bool {
	select {
	case <-ctx.Done():
		r.recordDrop(context.Background(), routerDropReasonContextCanceled)
		r.logger.Debug("context canceled, dropping message")
		return false
	default:
	}

	select {
	case r.ch <- message:
		r.recordBufferFill(ctx)
		return true
	default:
		r.recordDrop(ctx, routerDropReasonBufferFull)
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

func (r *messageRouter) recordDrop(ctx context.Context, reason string) {
	if ops, ok := r.dropAddOpsByReason[reason]; ok {
		r.droppedCounter.Add(ctx, 1, ops...)
		return
	}
	// Unregistered reason — fall back to per-call allocation so the metric
	// still records (no silent miss). Adding a new reason should also extend
	// routerDropAddOpsByReason to keep the hot path zero-alloc.
	r.droppedCounter.Add(ctx, 1,
		metric.WithAttributes(attribute.String("ssv.validator.router.drop_reason", reason)),
	)
}

func (r *messageRouter) recordBufferFill(ctx context.Context) {
	if r.bufferFillSampleTick.Add(1)%bufferFillSampleRate != 0 {
		return
	}
	r.bufferFillGauge.Record(ctx, int64(len(r.ch)))
}
