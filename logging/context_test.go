package logging

import (
	"context"
	"reflect"
	"testing"

	"github.com/bloxapp/ssv/utils/logex"
	"go.uber.org/zap"
)

func TestWithFromContext(t *testing.T) {
	t.Run("NamedCheck", func(t *testing.T) {
		ctx := context.Background()
		expected := logex.TestLogger(t)
		expected = expected.Named("test")
		ctx = WithContext(ctx, expected)

		actual := FromContext(ctx)
		if !reflect.DeepEqual(expected, actual) {
			t.Errorf("expected %v got %v", expected, actual)
		}
	})

	t.Run("EmptyCtx", func(t *testing.T) {
		ctx := context.Background()
		logex.TestLogger(t) // set the global logger
		expected := zap.L()

		actual := FromContext(ctx)
		if !reflect.DeepEqual(expected, actual) { // expect that the logger returned is the global logger
			t.Errorf("expected %v got %v", expected, actual)
		}
	})
}
