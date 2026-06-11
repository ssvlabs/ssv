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

// TestHookLibp2pLoggingRetrofitsExistingLogger covers the production scenario:
// every go-libp2p/IPFS package registers its subsystem logger via a package-level
// `var log = golog.Logger(...)` at init() time, long before p2p.Setup() calls the
// hook. The hook must therefore retrofit an already-created logger, not merely gate
// ones created afterward — which is the init-order independence the hook claims.
func TestHookLibp2pLoggingRetrofitsExistingLogger(t *testing.T) {
	// Create the subsystem logger BEFORE the hook runs, then pin it to a known
	// non-debug level so the post-hook assertion proves the retrofit happened
	// (independent of any level other tests in this package may have left behind).
	core := golog.Logger("swarm2").Desugar().Core()
	require.NoError(t, golog.SetLogLevel("swarm2", "error"))
	require.False(t, core.Enabled(zapcore.DebugLevel), "precondition: swarm2 starts above debug")

	require.NoError(t, SetGlobal("info", "capital", "json", nil))
	require.NoError(t, HookLibp2pLogging())

	// The same, already-created logger must now log at debug.
	require.True(t, core.Enabled(zapcore.DebugLevel),
		"hook must retrofit the pre-existing swarm2 logger to debug")
}
