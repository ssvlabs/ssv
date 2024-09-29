package discovery

import (
	"sync"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/pkg/errors"
	"github.com/ssvlabs/ssv/networkconfig"
	"go.uber.org/zap"
)

// forkingDV5Listener wraps a pre-fork and a post-fork listener.
// Before the fork, it performs operations on both services.
// Aftet the fork, it performs operations only on the post-fork service.
type forkingDV5Listener struct {
	logger           *zap.Logger
	preForkListener  listener
	postForkListener listener
	closeOnce        sync.Once
	netCfg           networkconfig.NetworkConfig
}

func newForkingDV5Listener(logger *zap.Logger, preFork, postFork listener, netConfig networkconfig.NetworkConfig) *forkingDV5Listener {
	return &forkingDV5Listener{
		logger:           logger,
		preForkListener:  preFork,
		postForkListener: postFork,
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

	preForkIterator := l.preForkListener.RandomNodes()
	postForkIterator := l.postForkListener.RandomNodes()
	iterator, err := NewPreAndPostForkIterator(preForkIterator, postForkIterator, 5)
	if err != nil {
		// Default to post-fork iterator on error.
		l.logger.Error("failed to create pre and post-fork iterator", zap.Error(err))
		return postForkIterator
	}
	return iterator
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

type preAndPostForkIterator struct {
	preForkIterator  annotatedIterator
	postForkIterator annotatedIterator
	preForkFrequency int
	node             *enode.Node
	cursor           int
	mutex            sync.Mutex
}

// NewPreAndPostForkIterator returns an iterator that yields from the given iterators
// in a round-robin fashion, where in 1 of preForkFrequency times the preForkIterator is invoked.
func NewPreAndPostForkIterator(
	preForkIterator enode.Iterator,
	postForkIterator enode.Iterator,
	preForkFrequency int,
) (enode.Iterator, error) {
	if preForkFrequency < 1 {
		return nil, errors.New("preForkFrequency must be at least 1")
	}
	return &preAndPostForkIterator{
		preForkFrequency: preForkFrequency,

		// Annotate iterators for metric collection.
		preForkIterator:  annotatedIterator{preForkIterator, "pre"},
		postForkIterator: annotatedIterator{postForkIterator, "post"},
	}, nil
}

func (i *preAndPostForkIterator) Close() {
	i.mutex.Lock()
	defer i.mutex.Unlock()

	i.preForkIterator.Close()
	i.postForkIterator.Close()
}

func (i *preAndPostForkIterator) Node() *enode.Node {
	i.mutex.Lock()
	defer i.mutex.Unlock()

	return i.node
}

func (i *preAndPostForkIterator) Next() bool {
	i.mutex.Lock()
	defer i.mutex.Unlock()

	i.cursor++
	if (i.cursor % i.preForkFrequency) == 0 {
		return i.firstNode(i.preForkIterator, i.postForkIterator)
	}
	return i.firstNode(i.postForkIterator, i.preForkIterator)
}

// firstNode picks the node from the first iterator that returns true for Next.
func (i *preAndPostForkIterator) firstNode(iterators ...annotatedIterator) (ok bool) {
	i.node = nil
	for _, iterator := range iterators {
		if iterator.Next() {
			i.node = iterator.Node()
			return true
		}
	}
	return false
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
	zap.L().Debug("iterated nodes", zap.String("fork", i.fork))
	metricIteratedNodes.WithLabelValues(i.fork).Inc()
	return true
}
