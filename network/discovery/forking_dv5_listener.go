package discovery

import (
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ssvlabs/ssv/networkconfig"
	"go.uber.org/zap"
)

const (
	defaultIteratorTimeout = 5 * time.Second
)

// forkingDV5Listener wraps a pre-fork and a post-fork listener.
// Before the fork, it performs operations on both services.
// Aftet the fork, it performs operations only on the post-fork service.
type forkingDV5Listener struct {
	logger           *zap.Logger
	preForkListener  Listener
	postForkListener Listener
	iteratorTimeout  time.Duration
	closeOnce        sync.Once
	netCfg           networkconfig.NetworkConfig
}

func NewForkingDV5Listener(logger *zap.Logger, preFork, postFork Listener, iteratorTimeout time.Duration, netConfig networkconfig.NetworkConfig) *forkingDV5Listener {
	if iteratorTimeout == 0 {
		iteratorTimeout = defaultIteratorTimeout
	}
	return &forkingDV5Listener{
		logger:           logger,
		preForkListener:  preFork,
		postForkListener: postFork,
		iteratorTimeout:  iteratorTimeout,
		netCfg:           netConfig,
	}
}

// Before the fork, returns the result of a Lookup in both pre and post-fork services.
// After the fork, returns only the result from the post-fork service.
func (l *forkingDV5Listener) Lookup(id enode.ID) []*enode.Node {
	if l.netCfg.PastAlanFork() {
		l.closePreForkListener()
		return l.postForkListener.Lookup(id)
	}

	nodes := l.postForkListener.Lookup(id)
	nodes = append(nodes, l.preForkListener.Lookup(id)...)
	return nodes
}

// Before the fork, returns an iterator for both pre and post-fork services.
// After the fork, returns only the iterator from the post-fork service.
func (l *forkingDV5Listener) RandomNodes() enode.Iterator {
	if l.netCfg.PastAlanFork() {
		l.closePreForkListener()
		return l.postForkListener.RandomNodes()
	}

	fairMix := enode.NewFairMix(l.iteratorTimeout)
	fairMix.AddSource(&annotatedIterator{l.postForkListener.RandomNodes(), "post"})
	fairMix.AddSource(&annotatedIterator{l.preForkListener.RandomNodes(), "pre"})
	return fairMix
}

// Before the fork, returns all nodes from the pre and post-fork listeners.
// After the fork, returns only the result from the post-fork service.
func (l *forkingDV5Listener) AllNodes() []*enode.Node {
	if l.netCfg.PastAlanFork() {
		l.closePreForkListener()
		return l.postForkListener.AllNodes()
	}

	enodes := l.postForkListener.AllNodes()
	enodes = append(enodes, l.preForkListener.AllNodes()...)
	return enodes
}

// Sends a ping in the post-fork service.
// Before the fork, it also tries to ping with the pre-fork service in case of error.
func (l *forkingDV5Listener) Ping(node *enode.Node) error {
	if l.netCfg.PastAlanFork() {
		l.closePreForkListener()
		return l.postForkListener.Ping(node)
	}

	err := l.postForkListener.Ping(node)
	if err != nil {
		return l.preForkListener.Ping(node)
	}
	return nil
}

// Returns the LocalNode using the post-fork listener.
// Both pre and post-fork listeners should have the same LocalNode.
func (l *forkingDV5Listener) LocalNode() *enode.LocalNode {
	if l.netCfg.PastAlanFork() {
		l.closePreForkListener()
		return l.postForkListener.LocalNode()
	}
	return l.postForkListener.LocalNode()
}

// Closes both listeners
func (l *forkingDV5Listener) Close() {
	l.closePreForkListener()
	l.postForkListener.Close()
}

// closePreForkListener ensures preForkListener is closed once
func (l *forkingDV5Listener) closePreForkListener() {
	l.closeOnce.Do(func() {
		l.preForkListener.Close()
	})
}

// annotatedIterator wraps an enode.Iterator with metrics collection.
type annotatedIterator struct {
	enode.Iterator
	fork string
}

func (i *annotatedIterator) Next() bool {
	if !i.Iterator.Next() {
		return false
	}
	metricIterations.WithLabelValues(i.fork).Inc()
	return true
}
