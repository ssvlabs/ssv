package logging

import (
	"go.uber.org/zap/zapcore"
)

var levelEncoder zapcore.LevelEncoder

// LevelEncoder takes a raw string (as []byte) and returnsthe corresponding zapcore.LevelEncoder
func LevelEncoder(raw []byte) zapcore.LevelEncoder {
	if err := levelEncoder.UnmarshalText(raw); err != nil {
		return nil
	}
	return levelEncoder
}
