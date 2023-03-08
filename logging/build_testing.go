package logging

import (
	"sync"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestLogger(t *testing.T) *zap.Logger {
	SetGlobalLogger(zapcore.DebugLevel)
	return zap.L().Named(t.Name())
}

func BenchLogger(b *testing.B) *zap.Logger {
	SetGlobalLogger(zapcore.DebugLevel)
	return zap.L().Named(b.Name())
}

// Reset the once init for logger
func Reset() {
	buildOnce = sync.Once{}
}
