package logging

import (
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func parseConfigLevel(levelName string) (zapcore.Level, error) {
	return zapcore.ParseLevel(levelName)
}

func parseConfigLevelEncoder(levelEncoderName string) zapcore.LevelEncoder {
	switch levelEncoderName {
	case "capitalColor":
		return zapcore.CapitalColorLevelEncoder
	case "capital":
		return zapcore.CapitalLevelEncoder
	case "lowercase":
		return zapcore.LowercaseLevelEncoder
	default:
		return zapcore.CapitalLevelEncoder
	}
}

var once sync.Once

func SetGlobalLogger(levelName string, levelEncoderName string, logFormat string, excludeServices []string, pubSubTrace bool) error {
	once.Do(func() {
		if err := setDebugServicesEncoder(logFormat, excludeServices, pubSubTrace); err != nil {
			panic(err)
		}
	})

	level, err := parseConfigLevel(levelName)
	if err != nil {
		return err
	}

	levelEncoder := parseConfigLevelEncoder(levelEncoderName)

	cfg := zap.Config{
		Encoding:    debugServicesEncoderName,
		Level:       zap.NewAtomicLevelAt(level),
		OutputPaths: []string{"stdout"},
		EncoderConfig: zapcore.EncoderConfig{
			MessageKey:  "message",
			LevelKey:    "level",
			EncodeLevel: levelEncoder,
			TimeKey:     "time",
			EncodeTime: func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
				enc.AppendString(t.UTC().Format("2006-01-02T15:04:05.000000Z"))
			},
			CallerKey:        "caller",
			EncodeCaller:     zapcore.ShortCallerEncoder,
			EncodeDuration:   zapcore.StringDurationEncoder,
			NameKey:          "name",
			ConsoleSeparator: "\t",
		},
	}

	globalLogger, err := cfg.Build()
	if err != nil {
		return fmt.Errorf("err making logger: %w", err)
	}

	zap.ReplaceGlobals(globalLogger)

	return nil
}
