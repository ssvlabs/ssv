package traces

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

var logger *zap.Logger

const traceIDByteLen = 16

func InitLogger(l *zap.Logger) {
	if l == nil {
		l = zap.NewNop()
	}
	logger = l
}

// Context returns a new context with a deterministic trace ID based on the input string.
// Useful for generating consistent trace IDs for the same logical operation (e.g., by duty ID),
// which helps in correlating spans across distributed by network components.
func Context(ctx context.Context, str string) context.Context {
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

// Errorf sets the status of the span to error and returns an error with the formatted message.
func Errorf(span trace.Span, f string, args ...any) error {
	err := fmt.Errorf(f, args...)
	span.SetStatus(codes.Error, err.Error())
	return err
}

func Error(span trace.Span, err error) error {
	span.SetStatus(codes.Error, err.Error())
	return err
}
