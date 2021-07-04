package tasks

import (
	"crypto/rand"
	"fmt"
	"github.com/bloxapp/ssv/pubsub"
	"go.uber.org/zap"
	"math/big"
	"sync"
	"time"
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
	sub     pubsub.Subject
	mut     sync.RWMutex
}

func newStopper(logger *zap.Logger) *stopper {
	s := stopper{sub: pubsub.NewSubject(logger), logger: logger}
	return &s
}

func (s *stopper) IsStopped() bool {
	s.mut.RLock()
	defer s.mut.RUnlock()

	return s.stopped
}

func (s *stopper) Chan() chan bool {
	cn, _ := s.sub.Register(chanID())
	res := make(chan bool, 1)
	go func() {
		<-cn
		res <- true
	}()
	return res
}

func (s *stopper) stop() {
	s.mut.Lock()
	defer s.mut.Unlock()

	s.stopped = true
	s.sub.Notify(struct{}{})
}

func chanID() string {
	var i int64
	r, err := rand.Int(rand.Reader, big.NewInt(2048))
	if err != nil {
		i = time.Now().UnixNano()
	} else {
		i = r.Int64()
	}
	return fmt.Sprintf("i-%d", i)
}
