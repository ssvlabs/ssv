package log

import (
	"os"
	"path/filepath"
	"testing"

	golog "github.com/ipfs/go-log/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
)

// resetGoLog restores go-log to a quiet default after the test (all subsystems
// at error, output discarded), so the global mutations these tests make — the
// raised subsystem levels and the replaced primary core — don't leak into other
// tests in this package.
func resetGoLog(t *testing.T) {
	t.Cleanup(func() {
		golog.SetupLogging(golog.Config{Level: golog.LevelError})
	})
}

func TestHookLibp2pLogging(t *testing.T) {
	resetGoLog(t)

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
	resetGoLog(t)

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

// TestHookLibp2pLoggingWritesThroughSSVFileSink exercises the production path the
// other tests skip — a configured file sink (production always runs with one). It
// proves, end to end through the real sink rather than via Core.Enabled, that libp2p
// logs are (a) rendered through the shared SSV file core in SSV's JSON format and
// (b) still gated by the per-subsystem levels.
func TestHookLibp2pLoggingWritesThroughSSVFileSink(t *testing.T) {
	resetGoLog(t)

	logFile := filepath.Join(t.TempDir(), "ssv.log")
	require.NoError(t, SetGlobal("info", "capital", "json", &LogFileOptions{FilePath: logFile}))
	require.NoError(t, HookLibp2pLogging())

	// swarm2 is raised to debug: its debug line reaches the file.
	golog.Logger("swarm2").Debug("swarm2-debug-9f3a")
	// An un-raised subsystem stays at error: debug is dropped, error passes.
	golog.Logger("some-other-subsystem").Debug("other-debug-9f3a")
	golog.Logger("some-other-subsystem").Error("other-error-9f3a")

	out, err := os.ReadFile(logFile)
	require.NoError(t, err)
	contents := string(out)

	// Rendered through the shared SSV file core (dev JSON encoder, "M" message key).
	require.Contains(t, contents, `"M":"swarm2-debug-9f3a"`, "swarm2 debug must reach the SSV file sink")
	require.Contains(t, contents, `"M":"other-error-9f3a"`, "libp2p error logs must reach the SSV file sink")
	require.NotContains(t, contents, "other-debug-9f3a", "debug from an un-raised subsystem must be gated out")
}
