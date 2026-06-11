package log

import (
	"fmt"
	"io"
	"log"
	"os"
	"runtime/debug"
	"sync"
	"time"

	golog "github.com/ipfs/go-log/v2"
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
	// Write is internally mutex-guarded, so the SSV and libp2p cores can share
	// this one instance without interleaving.
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

	globalLogConfigMu.Lock()
	globalEncoderConfig = encoderConfig
	globalLogFormat = logFormat
	globalFileSyncer = fileSyncer
	globalLogConfigMu.Unlock()

	zap.ReplaceGlobals(zap.New(core))

	return nil
}

// Logging config resolved by the most recent SetGlobal call, retained so
// go-libp2p logs can be routed through a matching core (see HookLibp2pLogging).
// Guarded by globalLogConfigMu since SetGlobal may run concurrently (e.g. tests).
var (
	globalLogConfigMu   sync.Mutex
	globalEncoderConfig zapcore.EncoderConfig
	globalLogFormat     string
	globalFileSyncer    zapcore.WriteSyncer
)

// stdoutSyncer is shared by every core built via assembleCore so concurrent
// writes from the SSV logger and the go-libp2p core don't interleave.
var stdoutSyncer = zapcore.Lock(zapcore.AddSync(os.Stdout))

// levelAll enables every level. Used where gating happens elsewhere: the always-on
// file sink, and the go-libp2p core whose per-subsystem levels do the gating.
var levelAll = zap.LevelEnablerFunc(func(zapcore.Level) bool { return true })

// assembleCore builds the SSV logging core: a stdout core gated by stdoutLevel,
// optionally tee'd with an always-on file core. The same builder is reused to
// route go-libp2p logs through an identical sink.
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

// HookLibp2pLogging routes go-libp2p's logs (github.com/ipfs/go-log/v2) through
// the SSV logger, with the swarm2 and basichost subsystems at debug and the rest
// at error. It surfaces libp2p connection/host diagnostics in SSV's own log
// format without the GOLOG_LOG_LEVEL environment variable. SetGlobal must run first.
//
// go-log holds its logging state in process-global variables, so this reconfigures
// libp2p logging for the entire process, not a single network instance. Calling it
// more than once (e.g. a second p2p network) simply re-applies the same policy.
func HookLibp2pLogging() error {
	globalLogConfigMu.Lock()
	encoderConfig, logFormat, fileSyncer := globalEncoderConfig, globalLogFormat, globalFileSyncer
	globalLogConfigMu.Unlock()

	if logFormat == "" {
		return fmt.Errorf("global logger not initialized")
	}

	// Seed go-log's per-subsystem levels and default the rest to error. Setting
	// them via SetupLogging (rather than SetLogLevel) registers the levels even
	// for subsystem loggers that haven't been created yet, so the policy holds
	// regardless of package init ordering. SetupLogging resets every subsystem
	// level first, so this deliberately overrides any GOLOG_LOG_LEVEL.
	golog.SetupLogging(golog.Config{
		Level: golog.LevelError,
		SubsystemLevels: map[string]golog.LogLevel{
			"swarm2":    golog.LevelDebug,
			"basichost": golog.LevelDebug,
		},
		// No Stdout/Stderr/File: the core SetupLogging builds writes nowhere and
		// is immediately replaced by the SSV core below.
	})

	// Route libp2p logs through a debug-floor core sharing the SSV logger's
	// encoder and writers; the per-subsystem levels above do the gating.
	core, err := assembleCore(levelAll, encoderConfig, logFormat, fileSyncer)
	if err != nil {
		return fmt.Errorf("build libp2p logging core: %w", err)
	}

	golog.SetPrimaryCore(core)

	return nil
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
