package log

import (
	"testing"

	golog "github.com/ipfs/go-log/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
)

func TestHookLibp2pLogging(t *testing.T) {
	require.NoError(t, SetGlobal("info", "capital", "json", nil))
	require.NoError(t, HookLibp2pLogging())

	// swarm2 and basichost are raised to debug.
	for _, subsystem := range []string{"swarm2", "basichost"} {
		core := golog.Logger(subsystem).Desugar().Core()
		require.True(t, core.Enabled(zapcore.DebugLevel), "%s should log at debug", subsystem)
	}

	// Any other subsystem stays at the error default: errors pass, debug is dropped.
	other := golog.Logger("some-other-subsystem").Desugar().Core()
	require.True(t, other.Enabled(zapcore.ErrorLevel))
	require.False(t, other.Enabled(zapcore.DebugLevel))
}
