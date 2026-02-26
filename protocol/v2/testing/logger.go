package testing

import (
	"os"
	stdtesting "testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/ssvlabs/ssv/observability/log"
)

const specTestLogLevelEnv = "SSV_SPECTEST_LOG_LEVEL"

func SpectestLogger(tb stdtesting.TB) *zap.Logger {
	tb.Helper()

	level := os.Getenv(specTestLogLevelEnv)
	if level == "" {
		level = zapcore.DPanicLevel.String()
	}

	err := log.SetGlobal(level, "capital", "console", nil)
	require.NoError(tb, err)
	return zap.L().Named(tb.Name())
}
