package log

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestLogger(t *testing.T) *zap.Logger {
	err := SetGlobalLogger(zapcore.DebugLevel.String(), "capital", "console", nil)
	require.NoError(t, err)
	return zap.L().Named(t.Name())
}
