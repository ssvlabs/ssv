package logging

import (
	"fmt"
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

// GetLoggerLevelValue resolves logger level to zap level
func GetLoggerLevelValue(loggerLevel string) (zapcore.Level, error) {
	switch loggerLevel {
	case "debug":
		return zapcore.DebugLevel, nil
	case "info":
		return zapcore.InfoLevel, nil
	case "warn":
		return zapcore.WarnLevel, nil
	case "error":
		return zapcore.ErrorLevel, nil
	case "dpanic":
		return zapcore.DPanicLevel, nil
	case "panic":
		return zapcore.PanicLevel, nil
	case "fatal":
		return zapcore.FatalLevel, nil
	default:
		return zapcore.InfoLevel, fmt.Errorf("unknown log level - %s", loggerLevel)
	}
}
