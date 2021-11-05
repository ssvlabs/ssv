package tasks

import (
	"github.com/bloxapp/ssv/pubsub"
	"go.uber.org/zap"
	"sync"
)

// Stopper represents the object used to stop running functions
// should be used by the running function, once stopped the function act accordingly
type Stopper interface {
	// IsStopped returns true if the stopper already stopped
	IsStopped() bool
	// Chan returns a bool channel to be notified once stopped
	Chan() chan bool
}

type stopper struct {
	logger  *zap.Logger
	stopped bool
	emitter pubsub.Emitter
	mut     sync.RWMutex
}

func newStopper(logger *zap.Logger) *stopper {
	s := stopper{emitter: pubsub.NewEmitter(), logger: logger}
	return &s
}

func (s *stopper) IsStopped() bool {
	s.mut.RLock()
	defer s.mut.RUnlock()

	return s.stopped
}

func (s *stopper) Chan() chan bool {
	res := make(chan bool, 1)
	s.emitter.Once("stop", func(data pubsub.EventData) {
		res <- true
	})
	return res
}

func (s *stopper) stop() {
	s.mut.Lock()
	defer s.mut.Unlock()

	s.stopped = true
	s.emitter.Notify("stop", stoppedEvent{})
}

type stoppedEvent struct{}

// Copy implements pubsub.EventData
func (se stoppedEvent) Copy() interface{} {
	return se
}
