package log

import (
	"reflect"
	"testing"

	"go.uber.org/zap"
)

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
