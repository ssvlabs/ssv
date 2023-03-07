package logging

import (
	"log"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func SetGlobalLogger(level zapcore.Level) {
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
				enc.AppendString(t.UTC().Format("2006-01-02T15:04:05.000000Z")) //converting time to ISO 3339 format
			},
			CallerKey:      "caller",
			EncodeCaller:   zapcore.ShortCallerEncoder,
			EncodeDuration: zapcore.StringDurationEncoder,
		},
	}

	globalLogger, err := cfg.Build()
	if err != nil {
		log.Fatalf("err making logger: %+v", err)
	}

	zap.ReplaceGlobals(globalLogger)
}
