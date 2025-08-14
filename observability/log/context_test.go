package log

import (
	"context"
	"reflect"
	"testing"

	"go.uber.org/zap"
)

// WithContext returns a context with the logger.
func WithContext(ctx context.Context, logger *zap.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

func TestWithFromContext(t *testing.T) {
	t.Run("NamedCheck", func(t *testing.T) {
		ctx := t.Context()
		expected := TestLogger(t)
		expected = expected.Named("test")
		ctx = WithContext(ctx, expected)

		actual := FromContext(ctx)
		if !reflect.DeepEqual(expected, actual) {
			t.Errorf("expected %v got %v", expected, actual)
		}
	})

	t.Run("EmptyCtx", func(t *testing.T) {
		ctx := t.Context()
		expected := zap.L()

		actual := FromContext(ctx)
		if !reflect.DeepEqual(expected, actual) { // expect that the logger returned is the global logger
			t.Errorf("expected %v got %v", expected, actual)
		}
	})
}
