//go:build testutils

// This file contains helpers for tests only.
// It will not be compiled into production binaries.

package fields

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func ENRStr(val string) zapcore.Field {
	return zap.String(FieldENR, val)
}
