package logex

import (
	"log"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Build builds the default zap logger, and sets the global zap logger to the configured logger instance.
func Build(appName string, level zapcore.Level) *zap.Logger {
	cfg := zap.Config{
		Encoding:    "console",
		Level:       zap.NewAtomicLevelAt(level),
		OutputPaths: []string{"stdout"},
		EncoderConfig: zapcore.EncoderConfig{
			MessageKey:  "message",
			LevelKey:    "level",
			EncodeLevel: zapcore.CapitalColorLevelEncoder,
			TimeKey:     "time",
			EncodeTime: func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
				enc.AppendString(iso3339CleanTime(t))
			},
			CallerKey:      "caller",
			EncodeCaller:   zapcore.ShortCallerEncoder,
			EncodeDuration: zapcore.StringDurationEncoder,
		},
	}

	logger, err := cfg.Build()
	if err != nil {
		log.Fatalf("err making logger: %+v", err)
	}

	logger = logger.With(zap.String("app", appName))
	zap.ReplaceGlobals(logger)
	return logger
}

// iso3339CleanTime converts the given time to ISO 3339 format
func iso3339CleanTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000000Z")
}
