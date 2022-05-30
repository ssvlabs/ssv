package logex

import "go.uber.org/zap"

// Logger is the logger interface used across the project
// it adds custom methods (e.g. Trace) on top of zap.Logger
type Logger interface {
	// With enables to add more fields to the underlying zap.Logger
	With(fields ...zap.Field) Logger
	// Trace is an additional function that is missing in zap.Logger
	Trace(msg string, fields ...zap.Field)
	// Debug calls the underlying zap.Logger
	Debug(msg string, fields ...zap.Field)
	// Info calls the underlying zap.Logger
	Info(msg string, fields ...zap.Field)
	// Warn calls the underlying zap.Logger
	Warn(msg string, fields ...zap.Field)
	// Error calls the underlying zap.Logger
	Error(msg string, fields ...zap.Field)
	// Fatal calls the underlying zap.Logger
	Fatal(msg string, fields ...zap.Field)
	// Panic calls the underlying zap.Logger
	Panic(msg string, fields ...zap.Field)
}

// NewSSVLogger creates a new Logger
func NewSSVLogger(base *zap.Logger) Logger {
	return &wrapper{base}
}

// wrapper implements Logger
type wrapper struct {
	logger *zap.Logger
}

func (w *wrapper) With(fields ...zap.Field) Logger {
	return NewSSVLogger(w.logger.With(fields...))
}

func (w *wrapper) Trace(msg string, fields ...zap.Field) {
	w.logger.Debug(msg, fields...)
}

func (w *wrapper) Debug(msg string, fields ...zap.Field) {
	w.logger.Debug(msg, fields...)
}

func (w *wrapper) Info(msg string, fields ...zap.Field) {
	w.logger.Info(msg, fields...)
}

func (w *wrapper) Warn(msg string, fields ...zap.Field) {
	w.logger.Warn(msg, fields...)
}

func (w *wrapper) Error(msg string, fields ...zap.Field) {
	w.logger.Error(msg, fields...)
}

func (w *wrapper) Fatal(msg string, fields ...zap.Field) {
	w.logger.Fatal(msg, fields...)
}

func (w *wrapper) Panic(msg string, fields ...zap.Field) {
	w.logger.Panic(msg, fields...)
}
