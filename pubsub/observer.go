package pubsub

import (
	"go.uber.org/zap"
	"sync"
)

const defaultChannelBuffer = 25

func newChannel(bufSize int) SubjectChannel {
	return make(SubjectChannel, bufSize)
}

// observer is an internal abstraction on top of channels
type observer struct {
	channel SubjectChannel
	active  bool
	mut     sync.Mutex
	logger  *zap.Logger
}

func newSubjectObserver(logger *zap.Logger) *observer {
	so := observer{
		newChannel(defaultChannelBuffer),
		true,
		sync.Mutex{},
		logger,
	}
	return &so
}

// isActive is a race-free way of checking observer activity
func (so *observer) isActive() bool {
	so.mut.Lock()
	defer so.mut.Unlock()

	return so.active
}

func (so *observer) close() {
	defer func() {
		if err := recover(); err != nil {
			// catch "close of closed channel"
			so.logger.Debug("recovering from panic", zap.Error(err.(error)))
		}
	}()

	so.mut.Lock()
	defer so.mut.Unlock()

	if so.active {
		so.active = false
		close(so.channel)
	}
}

func (so *observer) notifyCallback(e SubjectEvent) {
	so.mut.Lock()
	if !so.active {
		so.mut.Unlock()
		return
	}
	go func() {
		defer func() {
			if err := recover(); err != nil {
				// catch "send on closed channel"
				so.logger.Debug("recovering from panic", zap.Error(err.(error)))
			}
		}()
		// in case the channel is blocking - the observer should be locked
		// and therefore the lock is acquired again
		defer so.mut.Unlock()
		so.channel <- e
	}()
}
