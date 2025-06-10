package logger

import (
	"fmt"

	"go.uber.org/zap"
)

// SetupLogger creates a configured zap logger.
func SetupLogger(logLevel, logFormat string) (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()

	if logFormat == "console" {
		cfg.Encoding = "console"
		cfg.EncoderConfig = zap.NewDevelopmentEncoderConfig()
	} else {
		cfg.Encoding = "json"
	}

	level := zap.NewAtomicLevel()
	if err := level.UnmarshalText([]byte(logLevel)); err != nil {
		return nil, fmt.Errorf("parse log level: %w", err)
	}
	cfg.Level = level

	return cfg.Build()
}

// SetupDevelopmentLogger creates a development logger.
func SetupDevelopmentLogger() (*zap.Logger, error) {
	return zap.NewDevelopment()
}
