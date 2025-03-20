package kv

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/logging"
)

// setupLoggerTest creates a badgerLogger with an observer for testing.
func setupLoggerTest(t *testing.T) (*badgerLogger, *observer.ObservedLogs) {
	t.Helper()
	core, recorded := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	bLogger := newLogger(logger).(*badgerLogger)
	return bLogger, recorded
}

// Test_newLogger verifies that newLogger correctly creates a badgerLogger with proper naming.
func Test_newLogger(t *testing.T) {
	t.Parallel()

	t.Run("with regular logger", func(t *testing.T) {
		t.Parallel()

		logger := zap.NewExample()
		bl := newLogger(logger)

		require.NotNil(t, bl)
		require.IsType(t, &badgerLogger{}, bl)
		assert.Contains(t, bl.(*badgerLogger).logger.Name(), logging.NameBadgerDBLog)
	})

	t.Run("with nop logger", func(t *testing.T) {
		t.Parallel()

		logger := zap.NewNop()
		bl := newLogger(logger)

		require.NotNil(t, bl)
		require.IsType(t, &badgerLogger{}, bl)
	})
}

// TestBadgerLogger_Errorf verifies the Errorf method properly logs messages at error level.
func TestBadgerLogger_Errorf(t *testing.T) {
	t.Parallel()

	t.Run("simple message", func(t *testing.T) {
		t.Parallel()

		bl, logs := setupLoggerTest(t)

		bl.Errorf("test error message")

		require.Equal(t, 1, logs.Len())
		lastLog := logs.All()[0]
		assert.Equal(t, zapcore.ErrorLevel, lastLog.Level)
		assert.Equal(t, "test error message", lastLog.Message)
	})

	t.Run("with format args", func(t *testing.T) {
		t.Parallel()

		bl, logs := setupLoggerTest(t)

		bl.Errorf("test error with args: %s, %d", "string", 123)

		require.Equal(t, 1, logs.Len())
		lastLog := logs.All()[0]
		assert.Equal(t, zapcore.ErrorLevel, lastLog.Level)
		assert.Equal(t, "test error with args: string, 123", lastLog.Message)
	})

	t.Run("with empty message", func(t *testing.T) {
		t.Parallel()

		bl, logs := setupLoggerTest(t)

		bl.Errorf("")

		require.Equal(t, 1, logs.Len())
		lastLog := logs.All()[0]
		assert.Equal(t, zapcore.ErrorLevel, lastLog.Level)
		assert.Equal(t, "", lastLog.Message)
	})
}

// TestBadgerLogger_Warningf verifies the Warningf method properly logs messages at warning level.
func TestBadgerLogger_Warningf(t *testing.T) {
	t.Parallel()

	t.Run("simple message", func(t *testing.T) {
		t.Parallel()

		bl, logs := setupLoggerTest(t)

		bl.Warningf("test warning message")

		require.Equal(t, 1, logs.Len())
		lastLog := logs.All()[0]
		assert.Equal(t, zapcore.WarnLevel, lastLog.Level)
		assert.Equal(t, "test warning message", lastLog.Message)
	})

	t.Run("with format args", func(t *testing.T) {
		t.Parallel()

		bl, logs := setupLoggerTest(t)

		bl.Warningf("test warning with args: %s, %d", "string", 123)

		require.Equal(t, 1, logs.Len())
		lastLog := logs.All()[0]
		assert.Equal(t, zapcore.WarnLevel, lastLog.Level)
		assert.Equal(t, "test warning with args: string, 123", lastLog.Message)
	})
}

// TestBadgerLogger_Infof verifies the Infof method properly logs messages at info level.
func TestBadgerLogger_Infof(t *testing.T) {
	t.Parallel()

	t.Run("simple message", func(t *testing.T) {
		t.Parallel()

		bl, logs := setupLoggerTest(t)

		bl.Infof("test info message")

		require.Equal(t, 1, logs.Len())
		lastLog := logs.All()[0]
		assert.Equal(t, zapcore.InfoLevel, lastLog.Level)
		assert.Equal(t, "test info message", lastLog.Message)
	})

	t.Run("with format args", func(t *testing.T) {
		t.Parallel()

		bl, logs := setupLoggerTest(t)

		bl.Infof("test info with args: %s, %d", "string", 123)

		require.Equal(t, 1, logs.Len())
		lastLog := logs.All()[0]
		assert.Equal(t, zapcore.InfoLevel, lastLog.Level)
		assert.Equal(t, "test info with args: string, 123", lastLog.Message)
	})
}

// TestBadgerLogger_Debugf verifies the Debugf method properly logs messages at debug level.
func TestBadgerLogger_Debugf(t *testing.T) {
	t.Parallel()

	t.Run("simple message", func(t *testing.T) {
		t.Parallel()

		bl, logs := setupLoggerTest(t)

		bl.Debugf("test debug message")

		require.Equal(t, 1, logs.Len())
		lastLog := logs.All()[0]
		assert.Equal(t, zapcore.DebugLevel, lastLog.Level)
		assert.Equal(t, "test debug message", lastLog.Message)
	})

	t.Run("with format args", func(t *testing.T) {
		t.Parallel()

		bl, logs := setupLoggerTest(t)

		bl.Debugf("test debug with args: %s, %d", "string", 123)

		require.Equal(t, 1, logs.Len())
		lastLog := logs.All()[0]
		assert.Equal(t, zapcore.DebugLevel, lastLog.Level)
		assert.Equal(t, "test debug with args: string, 123", lastLog.Message)
	})
}
