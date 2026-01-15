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

// logger is defined as global var here to keep package API as simple as possible (instead of returning error we log them with this logger in some places)
var logger *zap.Logger

const traceIDByteLen = 16

// dutyIDKey is the context key for storing the duty ID string.
type dutyIDKey struct{}

func InitLogger(l *zap.Logger) {
	logger = l
}

// DutyIDFromContext retrieves the duty ID string from the context if present.
func DutyIDFromContext(ctx context.Context) (string, bool) {
	dutyID, ok := ctx.Value(dutyIDKey{}).(string)
	return dutyID, ok
}

// Context returns a new context with a deterministic trace ID based on the input string.
// It also stores the original string (typically a duty ID) for later retrieval via DutyIDFromContext.
// Useful for generating consistent trace IDs for the same logical operation (e.g., by duty ID),
// which helps in correlating spans across distributed by network components.
func Context(ctx context.Context, str string) context.Context {
	// Store the original string for later retrieval (e.g., for logging with duty ID).
	ctx = context.WithValue(ctx, dutyIDKey{}, str)

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
	return Error(span, err)
}

func Error(span trace.Span, err error) error {
	span.SetStatus(codes.Error, err.Error())
	return err
}
