package logging

import (
	"io"
	"log"
	"os"
	"runtime/debug"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// TODO: Log rotation out of the app
func getFileWriter(logFileName string) io.Writer {
	fileLogger := &lumberjack.Logger{
		Filename:   logFileName,
		MaxSize:    500, // megabytes
		MaxBackups: 3,
		MaxAge:     28, // days
		Compress:   false,
	}

	return fileLogger
}

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

func SetGlobalLogger(levelName string, levelEncoderName string, logFormat string, logFilePath string) error {
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

	consoleCore := zapcore.NewCore(zapcore.NewConsoleEncoder(cfg.EncoderConfig), os.Stdout, lv)

	if logFilePath == "" {
		zap.ReplaceGlobals(zap.New(consoleCore))
		return nil
	}

	logFileWriter := getFileWriter(logFilePath)

	lv2 := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return true // debug log returns all logs
	})

	dev := zapcore.NewJSONEncoder(zap.NewDevelopmentEncoderConfig())
	fileCore := zapcore.NewCore(dev, zapcore.AddSync(logFileWriter), lv2)

	zap.ReplaceGlobals(zap.New(zapcore.NewTee(consoleCore, fileCore)))

	return nil
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
