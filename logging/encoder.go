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
	childEncoder         zapcore.Encoder
	loggerNameSubstrings []string
}

func SetDebugServicesEncoder(logFormat string, debugServices []string) {
	err := zap.RegisterEncoder(debugServicesEncoderName, func(config zapcore.EncoderConfig) (zapcore.Encoder, error) {

		var enc zapcore.Encoder
		switch logFormat {
		case "console":
			enc = zapcore.NewConsoleEncoder(config)
		case "json":
			enc = zapcore.NewJSONEncoder(config)
		default:
			return nil, fmt.Errorf("invalid log level format: %s", logFormat)
		}

		return debugServicesEncoder{
			Encoder:              enc,
			childEncoder:         enc,
			loggerNameSubstrings: debugServices,
		}, nil
	})
	if err != nil {
		panic(err)
	}
}

func (d debugServicesEncoder) EncodeEntry(entry zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	for _, loggerNameSubstring := range d.loggerNameSubstrings {
		if strings.Contains(entry.LoggerName, loggerNameSubstring) {
			entry.Level = zap.DebugLevel
			break
		}
	}

	return d.childEncoder.EncodeEntry(entry, fields)
}
