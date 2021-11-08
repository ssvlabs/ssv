package tasks

import (
	"go.uber.org/zap"
	"sync"
)

// Stopper represents the object used to stop running functions
// should be used by the running function, once stopped the function act accordingly
type Stopper interface {
	// IsStopped returns true if the stopper already stopped
	IsStopped() bool
}

type stopper struct {
	logger  *zap.Logger
	stopped bool
	mut     sync.Mutex
}

func newStopper(logger *zap.Logger) *stopper {
	s := stopper{
		logger: logger,
		mut:    sync.Mutex{},
	}
	return &s
}

func (s *stopper) IsStopped() bool {
	s.mut.Lock()
	defer s.mut.Unlock()

	return s.stopped
}

func (s *stopper) stop() {
	s.mut.Lock()
	defer s.mut.Unlock()

	s.stopped = true
}
