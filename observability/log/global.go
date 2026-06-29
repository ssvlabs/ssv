package log

import (
	"fmt"
	"io"
	"log"
	"os"
	"runtime/debug"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
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

func SetGlobal(levelName string, levelEncoderName string, logFormat string, fileOptions *LogFileOptions) (err error) {
	defer func() {
		if err == nil {
			zap.L().Debug("logger is ready",
				zap.String("level", levelName),
				zap.String("encoder", levelEncoderName),
				zap.String("format", logFormat),
				zap.Any("file_options", fileOptions),
			)
		}
	}()
	level, err := parseConfigLevel(levelName)
	if err != nil {
		return err
	}

	levelEncoder := parseConfigLevelEncoder(levelEncoderName)

	encoderConfig := zapcore.EncoderConfig{
		MessageKey:  "msg",
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
	}

	// Unlike stdoutSyncer, the file syncer needs no zapcore.Lock: lumberjack's
	// Write is internally mutex-guarded.
	var fileSyncer zapcore.WriteSyncer
	if fileOptions != nil {
		fileSyncer = zapcore.AddSync(fileOptions.writer())
	}

	stdoutLevel := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl >= level
	})

	core, err := assembleCore(stdoutLevel, encoderConfig, logFormat, fileSyncer)
	if err != nil {
		return err
	}

	zap.ReplaceGlobals(zap.New(core))

	return nil
}

// stdoutSyncer is zapcore.Lock-wrapped so concurrent writes to stdout don't interleave.
var stdoutSyncer = zapcore.Lock(zapcore.AddSync(os.Stdout))

// levelAll enables every level; used for the always-on file sink, where gating happens elsewhere.
var levelAll = zap.LevelEnablerFunc(func(zapcore.Level) bool { return true })

// assembleCore builds the SSV logging core: a stdout core gated by stdoutLevel,
// optionally tee'd with an always-on file core.
func assembleCore(stdoutLevel zapcore.LevelEnabler, encoderConfig zapcore.EncoderConfig, logFormat string, fileSyncer zapcore.WriteSyncer) (zapcore.Core, error) {
	var stdoutEncoder zapcore.Encoder
	switch logFormat {
	case "console":
		stdoutEncoder = zapcore.NewConsoleEncoder(encoderConfig)
	case "json":
		stdoutEncoder = zapcore.NewJSONEncoder(encoderConfig)
	default:
		return nil, fmt.Errorf("unknown log format: %s", logFormat)
	}

	core := zapcore.NewCore(stdoutEncoder, stdoutSyncer, stdoutLevel)

	if fileSyncer != nil {
		dev := zapcore.NewJSONEncoder(zap.NewDevelopmentEncoderConfig())
		fileCore := zapcore.NewCore(dev, fileSyncer, levelAll) // file sink records all levels
		core = zapcore.NewTee(core, fileCore)
	}

	return core, nil
}

type LogFileOptions struct {
	FilePath   string
	MaxSize    int
	MaxBackups int
}

func (o *LogFileOptions) writer() io.Writer {
	return &lumberjack.Logger{
		Filename:   o.FilePath,
		MaxSize:    o.MaxSize, // megabytes
		MaxBackups: o.MaxBackups,
		MaxAge:     28, // days
		Compress:   false,
	}
}

func CapturePanic(logger *zap.Logger) {
	if r := recover(); r != nil {
		defer func() {
			if err := logger.Sync(); err != nil {
				log.Println("failed to sync zap.Logger", err)
			}
		}()
		stackTrace := string(debug.Stack())
		logger.Fatal("Recovered from panic", zap.Any("panic", r), zap.String("stackTrace", stackTrace))
	}
}
