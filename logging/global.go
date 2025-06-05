package logging

import (
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

func SetGlobalLogger(levelName string, levelEncoderName string, logFormat string, fileOptions *LogFileOptions) (err error) {
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

	lv := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl >= level
	})

	cfg := zap.Config{
		Encoding:    logFormat,
		Level:       zap.NewAtomicLevelAt(level),
		OutputPaths: []string{"stdout"},
		EncoderConfig: zapcore.EncoderConfig{
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
		},
	}

	var usedcore zapcore.Core

	switch logFormat {
	case "console":
		usedcore = zapcore.NewCore(zapcore.NewConsoleEncoder(cfg.EncoderConfig), os.Stdout, lv)
	case "json":
		usedcore = zapcore.NewCore(zapcore.NewJSONEncoder(cfg.EncoderConfig), os.Stdout, lv)
	}

	if fileOptions == nil {
		zap.ReplaceGlobals(zap.New(usedcore))
		return nil
	}

	lv2 := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return true // debug log returns all logs
	})

	dev := zapcore.NewJSONEncoder(zap.NewDevelopmentEncoderConfig())
	fileWriter := fileOptions.writer(fileOptions)
	fileCore := zapcore.NewCore(dev, zapcore.AddSync(fileWriter), lv2)

	zap.ReplaceGlobals(zap.New(zapcore.NewTee(usedcore, fileCore)))
	return nil
}

type LogFileOptions struct {
	FilePath   string
	MaxSize    int
	MaxBackups int
}

func (o LogFileOptions) writer(options *LogFileOptions) io.Writer {
	return &lumberjack.Logger{
		Filename:   options.FilePath,
		MaxSize:    options.MaxSize, // megabytes
		MaxBackups: options.MaxBackups,
		MaxAge:     28, // days
		Compress:   false,
	}
}

func CapturePanic(logger *zap.Logger) {
	if r := recover(); r != nil {
		// defer logger.Sync()
		defer func() {
			if err := logger.Sync(); err != nil {
				log.Println("failed to sync zap.Logger", err)
			}
		}()
		stackTrace := string(debug.Stack())
		logger.Panic("Recovered from panic", zap.Any("panic", r), zap.String("stackTrace", stackTrace))
	}
}
