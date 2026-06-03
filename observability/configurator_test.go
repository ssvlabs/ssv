package observability_test

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"
)

// installSentinelMetricsLogger swaps metrics.logger to a sentinel observed logger and
// arranges restoration to the originally-installed logger on test completion. Returns
// the observed logs handle so the test can assert what (if anything) was emitted through
// metrics.logger during the test.
func installSentinelMetricsLogger(t *testing.T) *observer.ObservedLogs {
	t.Helper()
	original := metrics.Logger()
	core, observed := observer.New(zapcore.DebugLevel)
	metrics.InitLogger(zap.New(core))
	t.Cleanup(func() { metrics.InitLogger(original) })
	return observed
}

// restoreGlobalLogger captures zap.L() before mutations (e.g. via InitializeLogger) and
// restores it in cleanup so test ordering doesn't affect subsequent tests.
func restoreGlobalLogger(t *testing.T) {
	t.Helper()
	original := zap.L()
	t.Cleanup(func() { zap.ReplaceGlobals(original) })
}

// restoreMetricsLogger captures metrics.Logger() before mutations and restores it on
// cleanup. Use this alongside restoreGlobalLogger in any test that calls InitializeLogger
// (which mutates metrics.logger as a side effect).
func restoreMetricsLogger(t *testing.T) {
	t.Helper()
	original := metrics.Logger()
	t.Cleanup(func() { metrics.InitLogger(original) })
}

func TestInitializeLogger_Succeeds(t *testing.T) {
	restoreGlobalLogger(t)
	restoreMetricsLogger(t)

	err := observability.InitializeLogger("info", "lowercase", "console", "", 0, 0)
	require.NoError(t, err)

	// Sanity: zap.L() now produces a real logger rather than the no-op global.
	assert.NotEqual(t, zap.NewNop(), zap.L(), "InitializeLogger must replace the global logger")
}

func TestInitializeLogger_PropagatesToMetricsPackage(t *testing.T) {
	restoreGlobalLogger(t)
	// restoreMetricsLogger is implicit in installSentinelMetricsLogger's cleanup, which
	// captures-and-restores the original logger.

	// Pre-condition: install a sentinel into metrics so we can detect propagation
	// overwriting it. After InitializeLogger runs, the sentinel will be replaced with the
	// named global logger — we just need to observe that the package's logger field is
	// no longer our sentinel (i.e. InitializeLogger called metrics.InitLogger).
	sentinelObserved := installSentinelMetricsLogger(t)

	err := observability.InitializeLogger("info", "lowercase", "console", "", 0, 0)
	require.NoError(t, err)

	// Trigger a log via the metrics package logger. If propagation happened, the
	// sentinel core no longer receives it — it goes to the new global instead.
	metrics.RecordUint64Value(t.Context(), uint64(math.MaxInt64)+1, noopRecordF)
	assert.Zero(t, sentinelObserved.Len(),
		"InitializeLogger must propagate to metrics.logger (sentinel should no longer receive)")
}

func TestInitialize_WorksWithoutInitializeLogger(t *testing.T) {
	// No InitializeLogger call: Initialize's localLogger falls back to zap.L() which may
	// be a no-op or whatever global was last set. Either way, Initialize must not panic
	// and must return a usable shutdown func.
	shutdown, err := observability.Initialize(t.Context(), "test-app", "test-version")
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	require.NoError(t, shutdown(t.Context()))
}

// TestInitialize_DoesNotReplaceMetricsLogger locks the contract documented at
// observability/configurator.go: Initialize intentionally does not re-propagate the
// logger, so calling it (e.g. mid-startup after deferred init) does not have the
// surprising side effect of replacing the metrics/traces internal loggers.
func TestInitialize_DoesNotReplaceMetricsLogger(t *testing.T) {
	restoreGlobalLogger(t)

	// Install a sentinel directly into the metrics package logger. Observable logs that
	// land in the sentinel core prove the package is still using it after Initialize.
	sentinelObserved := installSentinelMetricsLogger(t)

	shutdown, err := observability.Initialize(t.Context(), "test-app", "test-version")
	require.NoError(t, err)
	t.Cleanup(func() { _ = shutdown(t.Context()) })

	// Trigger an error log via the metrics package logger — value > MaxInt64 hits the
	// "value exceeds int64 range" log path in RecordUint64Value.
	metrics.RecordUint64Value(t.Context(), uint64(math.MaxInt64)+1, noopRecordF)

	require.Equal(t, 1, sentinelObserved.Len(),
		"Initialize must not replace metrics.logger (sentinel should still receive)")
	entry := sentinelObserved.All()[0]
	assert.Equal(t, zapcore.ErrorLevel, entry.Level)
	assert.Contains(t, entry.Message, "value exceeds int64 range")
}

func noopRecordF(_ context.Context, _ int64, _ ...metric.RecordOption) {}
