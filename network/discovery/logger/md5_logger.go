package compatible_logger

import (
	"context"
	"log/slog"
	"runtime"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Option struct {
	// log level (default: debug)
	Level slog.Leveler

	// optional: zap logger (default: zap.L())
	Logger *zap.Logger

	// optional: customize json payload builder
	Converter Converter

	// optional: see slog.HandlerOptions
	AddSource   bool
	ReplaceAttr func(groups []string, a slog.Attr) slog.Attr
}

var levelMap = map[slog.Level]zapcore.Level{
	slog.LevelDebug: zap.DebugLevel,
	slog.LevelInfo:  zap.InfoLevel,
	slog.LevelWarn:  zap.WarnLevel,
	slog.LevelError: zap.ErrorLevel,
}

func (o Option) NewZapHandler() slog.Handler {
	if o.Level == nil {
		o.Level = slog.LevelDebug
	}

	if o.Logger == nil {
		// should be selected lazily ?
		o.Logger = zap.L()
	}

	return &ZapHandler{
		option: o,
		attrs:  []slog.Attr{},
		groups: []string{},
	}
}

var _ slog.Handler = (*ZapHandler)(nil)

type ZapHandler struct {
	option Option
	attrs  []slog.Attr
	groups []string
}

func (h *ZapHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.option.Level.Level()
}

func (h *ZapHandler) Handle(ctx context.Context, record slog.Record) error {
	converter := DefaultConverter
	if h.option.Converter != nil {
		converter = h.option.Converter
	}

	level := levelMap[record.Level]
	fields := converter(h.option.AddSource, h.option.ReplaceAttr, h.attrs, h.groups, &record)

	checked := h.option.Logger.Check(level, record.Message)
	if checked != nil {
		if h.option.AddSource {
			frame, _ := runtime.CallersFrames([]uintptr{record.PC}).Next()
			checked.Caller = zapcore.NewEntryCaller(0, frame.File, frame.Line, true)
			checked.Stack = "" //@TODO
		} else {
			checked.Caller = zapcore.EntryCaller{}
			checked.Stack = ""
		}
		checked.Write(fields...)
		return nil
	} else {
		h.option.Logger.Log(level, record.Message, fields...)
	}

	return nil
}

func (h *ZapHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ZapHandler{
		option: h.option,
		attrs:  AppendAttrsToGroup(h.groups, h.attrs, attrs...),
		groups: h.groups,
	}
}

func (h *ZapHandler) WithGroup(name string) slog.Handler {
	return &ZapHandler{
		option: h.option,
		attrs:  h.attrs,
		groups: append(h.groups, name),
	}
}

var SourceKey = "source"
var ErrorKeys = []string{"error", "err"}

type Converter func(addSource bool, replaceAttr func(groups []string, a slog.Attr) slog.Attr, loggerAttr []slog.Attr, groups []string, record *slog.Record) []zapcore.Field

func DefaultConverter(addSource bool, replaceAttr func(groups []string, a slog.Attr) slog.Attr, loggerAttr []slog.Attr, groups []string, record *slog.Record) []zapcore.Field {
	// aggregate all attributes
	attrs := AppendRecordAttrsToAttrs(loggerAttr, groups, record)

	// developer formatters
	attrs = ReplaceError(attrs, ErrorKeys...)
	if addSource {
		attrs = append(attrs, Source(SourceKey, record))
	}
	attrs = ReplaceAttrs(replaceAttr, []string{}, attrs...)

	// handler formatter
	fields := AttrsToMap(attrs...)

	output := []zapcore.Field{}
	for k, v := range fields {
		output = append(output, zap.Any(k, v))
	}

	return output
}
