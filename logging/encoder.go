package logging

import (
	"fmt"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

const (
	debugServicesEncoderName = "debugServices"
)

type debugServicesEncoder struct {
	zapcore.Encoder
	excludeLoggers []string
}

func setDebugServicesEncoder(logFormat string, excludeLoggers []string, pubSubTrace bool) error {
	return zap.RegisterEncoder(debugServicesEncoderName, func(config zapcore.EncoderConfig) (zapcore.Encoder, error) {

		var enc zapcore.Encoder
		switch logFormat {
		case "console":
			enc = zapcore.NewConsoleEncoder(config)
		case "json":
			enc = zapcore.NewJSONEncoder(config)
		default:
			return nil, fmt.Errorf("invalid log level format: %s", logFormat)
		}

		if !pubSubTrace {
			excludeLoggers = append(excludeLoggers, NamePubsubTrace)
		}

		return debugServicesEncoder{
			Encoder:        enc,
			excludeLoggers: excludeLoggers,
		}, nil
	})
}

func (d debugServicesEncoder) EncodeEntry(entry zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	if entry.Level == zap.DebugLevel {
		for _, loggerNameSubstring := range d.excludeLoggers {
			if strings.Contains(entry.LoggerName, loggerNameSubstring) {
				return buffer.NewPool().Get(), nil
			}
		}
	}

	return d.Encoder.EncodeEntry(entry, fields)
}

func (d debugServicesEncoder) Clone() zapcore.Encoder {
	enc := d.Encoder.Clone()
	return debugServicesEncoder{
		Encoder:        enc,
		excludeLoggers: d.excludeLoggers,
	}
}
