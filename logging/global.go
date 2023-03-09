package logging

import (
	"fmt"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func parseConfigLevel(levelName string) (zapcore.Level, error) {
	return zapcore.ParseLevel(levelName)
}

func parseConfigLevelEncoder(levelEncoderName string) zapcore.LevelEncoder {
	switch levelEncoderName {
	case "capital":
		return zapcore.CapitalLevelEncoder
	case "lowercase":
		return zapcore.LowercaseLevelEncoder
	default:
		return zapcore.CapitalColorLevelEncoder
	}
}

// LevelEncoder: logex.LevelEncoder([]byte(cfg.LogLevelFormat)),
func SetGlobalLogger(levelName string, levelEncoderName string) error {
	level, err := parseConfigLevel(levelName)
	if err != nil {
		return err
	}

	levelEncoder := parseConfigLevelEncoder(levelEncoderName)

	cfg := zap.Config{
		Encoding:    "console",
		Level:       zap.NewAtomicLevelAt(level),
		OutputPaths: []string{"stdout"},
		EncoderConfig: zapcore.EncoderConfig{
			MessageKey:     "message",
			LevelKey:       "level",
			EncodeLevel:    levelEncoder,
			TimeKey:        "time",
			EncodeTime:     zapcore.RFC3339TimeEncoder,
			CallerKey:      "caller",
			EncodeCaller:   zapcore.ShortCallerEncoder,
			EncodeDuration: zapcore.StringDurationEncoder,
			NameKey:        "name",
		},
	}

	globalLogger, err := cfg.Build()
	if err != nil {
		return fmt.Errorf("err making logger: %w", err)
	}

	zap.ReplaceGlobals(globalLogger)

	return nil
}
