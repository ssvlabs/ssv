package logging

import (
	"sync"
	"testing"

	"go.uber.org/zap"
)

func TestLogger(t *testing.T) *zap.Logger {
	return Build(t.Name(), zap.DebugLevel, nil)
}

// Reset the once init for logger
func Reset() {
	buildOnce = sync.Once{}
}
