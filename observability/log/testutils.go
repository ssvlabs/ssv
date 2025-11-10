package log

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestLogger(t *testing.T) *zap.Logger {
	err := SetGlobal(zapcore.DebugLevel.String(), "capital", "console", nil)
	require.NoError(t, err)
	return zap.L().Named(t.Name())
}

func BenchLogger(b *testing.B) *zap.Logger {
	err := SetGlobal(zapcore.DebugLevel.String(), "capital", "console", nil)
	require.NoError(b, err)
	return zap.L().Named(b.Name())
}
