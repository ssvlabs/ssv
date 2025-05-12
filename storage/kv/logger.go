package kv

import (
	"fmt"

	"github.com/dgraph-io/badger/v4"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
)

// badgerLogger is a wrapper for badger.Logger
type badgerLogger struct {
	logger *zap.Logger
}

// newLogger creates a new instance of logger
func newLogger(l *zap.Logger) badger.Logger {
	return &badgerLogger{l.Named(logging.NameBadgerDBLog)}
}

// Errorf implements badger.Logger
func (bl *badgerLogger) Errorf(s string, i ...interface{}) {
	bl.logger.Error(fmt.Sprintf(s, i...))
}

// Warningf implements badger.Logger
func (bl *badgerLogger) Warningf(s string, i ...interface{}) {
	bl.logger.Warn(fmt.Sprintf(s, i...))
}

// Infof implements badger.Logger
func (bl *badgerLogger) Infof(s string, i ...interface{}) {
	bl.logger.Info(fmt.Sprintf(s, i...))
}

// Debugf implements badger.Logger
func (bl *badgerLogger) Debugf(s string, i ...interface{}) {
	bl.logger.Debug(fmt.Sprintf(s, i...))
}
