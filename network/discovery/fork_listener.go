package discovery

import (
	"sync"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ssvlabs/ssv/networkconfig"
)

// forkListener wraps a pre-fork and a post-fork listener.
// Before the fork, it performs operations on both services.
// Aftet the fork, it performs operations only on the post-fork service.
type forkListener struct {
	preForkListener  listener
	postForkListener listener
	closeOnce        sync.Once
	netCfg           networkconfig.NetworkConfig
}

func newForkListener(preFork, postFork listener, netConfig networkconfig.NetworkConfig) *forkListener {
	return &forkListener{
		preForkListener:  preFork,
		postForkListener: postFork,
		netCfg:           netConfig,
	}
}

// Before the fork, returns the result of a Lookup in both pre and post-fork services.
// After the fork, returns only the result from the post-fork service.
func (l *forkListener) Lookup(id enode.ID) []*enode.Node {
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
func (l *forkListener) RandomNodes() enode.Iterator {
	if l.netCfg.PastAlanFork() {
		l.closePreForkListener()
		return l.postForkListener.RandomNodes()
	}

	preForkIterator := l.preForkListener.RandomNodes()
	postForkIterator := l.postForkListener.RandomNodes()
	return NewPreAndPostForkIterator(preForkIterator, postForkIterator)
}

// Before the fork, returns all nodes from the pre and post-fork listeners.
// After the fork, returns only the result from the post-fork service.
func (l *forkListener) AllNodes() []*enode.Node {
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
func (l *forkListener) Ping(node *enode.Node) error {
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
func (l *forkListener) LocalNode() *enode.LocalNode {
	if l.netCfg.PastAlanFork() {
		l.closePreForkListener()
		return l.postForkListener.LocalNode()
	}
	return l.postForkListener.LocalNode()
}

// Closes both listeners
func (l *forkListener) Close() {
	l.closePreForkListener()
	l.postForkListener.Close()
}

// closePreForkListener ensures preForkListener is closed once
func (l *forkListener) closePreForkListener() {
	l.closeOnce.Do(func() {
		l.preForkListener.Close()
	})
}

// preAndPostForkIterator is an Iterator wrapper for enode.Iterator
// It gives preference to the postForkIterator, starting by it
// After the postForkIterator ends, it starts using the preForkIterator
type preAndPostForkIterator struct {
	preForkIterator             enode.Iterator
	postForkIterator            enode.Iterator
	hasChangedToPreForkIterator bool
	mutex                       sync.Mutex
}

func NewPreAndPostForkIterator(preForkIterator enode.Iterator, postForkIterator enode.Iterator) enode.Iterator {
	return &preAndPostForkIterator{
		preForkIterator:             preForkIterator,
		postForkIterator:            postForkIterator,
		hasChangedToPreForkIterator: false,
		mutex:                       sync.Mutex{},
	}
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

	if i.hasChangedToPreForkIterator {
		return i.preForkIterator.Node()
	} 
	
	return i.postForkIterator.Node()
}

// Check if the postForkIterator has a next element.
// Once postForkIterator ends, switches to checking the preForkIterator.
func (i *preAndPostForkIterator) Next() bool {
	i.mutex.Lock()
	defer i.mutex.Unlock()

	if i.hasChangedToPreForkIterator {
		return i.preForkIterator.Next()
	}

	hasNext := i.postForkIterator.Next()
	if hasNext {
		return true
	} else {
		i.hasChangedToPreForkIterator = true
		return i.preForkIterator.Next()
	}
}
