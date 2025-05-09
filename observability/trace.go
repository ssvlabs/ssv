package observability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

const traceIDByteLen = 16

func TraceContext(ctx context.Context, str string) context.Context {
	traceStrSha := sha256.Sum256([]byte(str))

	var traceID trace.TraceID
	traceID, err := trace.TraceIDFromHex(hex.EncodeToString(traceStrSha[:traceIDByteLen]))
	if err != nil {
		logger.Error("could not construct trace ID", zap.Error(err), zap.String("duty_id", str))
		return ctx
	}

	return trace.ContextWithSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
	}))
}
