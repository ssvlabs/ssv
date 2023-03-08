package logging

import (
	"log"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var buildOnce sync.Once

// EncodingConfig represents the needed encoding configuration for logger
type EncodingConfig struct {
	// Format
	Format string
	// LevelEncoder defines how level is encoded (colors, lowercase, etc.)
	LevelEncoder zapcore.LevelEncoder // lowercase
}

func defaultEncodingConfig(ec *EncodingConfig) *EncodingConfig {
	if ec == nil {
		ec = &EncodingConfig{}
	}
	if len(ec.Format) == 0 {
		ec.Format = "console"
	}
	if ec.LevelEncoder == nil {
		ec.LevelEncoder = zapcore.CapitalColorLevelEncoder
	}
	return ec
}

// Build builds the default zap logger, and sets the global zap logger to the configured logger instance.
func Build(appName string, level zapcore.Level, ec *EncodingConfig) *zap.Logger {
	ec = defaultEncodingConfig(ec)
	cfg := zap.Config{
		Encoding:    ec.Format,
		Level:       zap.NewAtomicLevelAt(level),
		OutputPaths: []string{"stdout"},
		EncoderConfig: zapcore.EncoderConfig{
			MessageKey:     "message",
			LevelKey:       "level",
			EncodeLevel:    ec.LevelEncoder,
			TimeKey:        "time",
			EncodeTime:     zapcore.RFC3339TimeEncoder,
			CallerKey:      "caller",
			EncodeCaller:   zapcore.ShortCallerEncoder,
			EncodeDuration: zapcore.StringDurationEncoder,
		},
	}

	logger, err := cfg.Build()
	if err != nil {
		log.Fatalf("err making logger: %+v", err)
	}

	// HACK: callers of Build don't know if it has been called/they don't know if Build will set the zap global logger
	// which means Build has indeterminate behavior
	buildOnce.Do(func() {
		zap.ReplaceGlobals(logger)
	})

	return logger
}
