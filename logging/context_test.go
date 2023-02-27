package logging

import (
	"context"
	"go.uber.org/zap"
	"reflect"
	"testing"
)

func TestWithFromContext(t *testing.T) {
	t.Run("NamedCheck", func(t *testing.T) {
		ctx := context.Background()
		expected := zap.L()
		expected = expected.Named("test")
		ctx = WithContext(ctx, expected)

		actual := FromContext(ctx)
		if !reflect.DeepEqual(expected, actual) {
			t.Errorf("expected %v got %v", expected, actual)
		}
	})

	t.Run("EmptyCtx", func(t *testing.T) {
		ctx := context.Background()
		expected := zap.L()

		actual := FromContext(ctx)
		if !reflect.DeepEqual(expected, actual) {
			t.Errorf("expected %v got %v", expected, actual)
		}
	})
}
