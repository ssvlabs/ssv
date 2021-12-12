package listeners

import (
	"context"
	"crypto/rand"
	"fmt"
	"github.com/bloxapp/ssv/network"
	"go.uber.org/zap"
	"math/big"
	"sync"
	"time"
)

// Container is an interface for of listeners container
type Container interface {
	// Register registers a new listener and returns a function for de-registration
	Register(l *Listener) func()
	// GetListeners returns all active listeners
	GetListeners(msgType network.NetworkMsg) []*Listener
}

// listeners is the data structure where listeners are saved
type listeners map[network.NetworkMsg][]*Listener

type listenersContainer struct {
	ctx       context.Context
	lock      *sync.RWMutex
	logger    *zap.Logger
	listeners listeners
}

// NewListenersContainer creates a new instance of listeners container
func NewListenersContainer(ctx context.Context, logger *zap.Logger) Container {
	return &listenersContainer{
		ctx:       ctx,
		lock:      &sync.RWMutex{},
		logger:    logger,
		listeners: make(listeners),
	}
}

// Register registers a new listener and returns a function for de-registration
func (lc *listenersContainer) Register(l *Listener) func() {
	lc.addListener(l)

	return func() {
		lc.removeListener(l.msgType, l.id)
	}
}

// GetListeners returns listeners of the given msg type
func (lc *listenersContainer) GetListeners(msgType network.NetworkMsg) []*Listener {
	lc.lock.RLock()
	defer lc.lock.RUnlock()

	lss, ok := lc.listeners[msgType]
	if !ok {
		return []*Listener{}
	}
	// copy to avoid data races
	res := make([]*Listener, len(lss))
	copy(res, lss)
	return res
}

// addListener adds a new listener to its list
func (lc *listenersContainer) addListener(l *Listener) {
	r, err := rand.Int(rand.Reader, new(big.Int).SetInt64(int64(1000000)))
	if err != nil {
		lc.logger.Error("could not create random number for listener")
		return
	}

	lc.lock.Lock()
	defer lc.lock.Unlock()

	lss, ok := lc.listeners[l.msgType]
	if !ok {
		lss = make([]*Listener, 0)
	}
	id := fmt.Sprintf("%d:%d:%d", len(lss), time.Now().UnixNano(), r.Int64())
	l.id = id
	lss = append(lss, l)
	lc.listeners[l.msgType] = lss
}

// removeListener removes a listener
func (lc *listenersContainer) removeListener(msgType network.NetworkMsg, id string) {
	lc.lock.Lock()
	defer lc.lock.Unlock()

	ls, ok := lc.listeners[msgType]
	if !ok {
		return
	}
	for i, l := range ls {
		if l.id == id {
			lc.listeners[msgType] = append(ls[:i], ls[i+1:]...)
			return
		}
	}
}
