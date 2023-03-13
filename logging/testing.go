package logging

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestLogger(t *testing.T) *zap.Logger {
	err := SetGlobalLogger("debug", "capital", "console", nil, false)
	require.NoError(t, err)
	return zap.L().Named(t.Name())
}

func BenchLogger(b *testing.B) *zap.Logger {
	err := SetGlobalLogger("debug", "capital", "console", nil, false)
	require.NoError(b, err)
	return zap.L().Named(b.Name())
}
