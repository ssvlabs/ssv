package discovery

import (
	"sync"

	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/enode"
)

// forkListener wraps a pre-fork and a post-fork listener
type forkListener struct {
	preForkListener  *discover.UDPv5
	postForkListener *discover.UDPv5
	closeOnce        sync.Once
}

func newForkListener(preFork, postFork *discover.UDPv5) *forkListener {
	return &forkListener{
		preForkListener:  preFork,
		postForkListener: postFork,
	}
}

// Returns the result of a Lookup in both pre and post-fork services
func (l *forkListener) Lookup(id enode.ID) []*enode.Node {
	// TODO: Uncomment the code below for the release that deprecates the pre-fork discovery service
	// if l.netCfg.PastAlanFork() {
	// 	l.closePreForkListener()
	//	return l.postForkListener.Lookup(id)
	// }

	nodes := l.postForkListener.Lookup(id)
	nodes = append(nodes, l.preForkListener.Lookup(id)...)
	return nodes
}

// Returns an iterator for both pre and post-fork services
func (l *forkListener) RandomNodes() enode.Iterator {
	// TODO: Uncomment the code below for the release that deprecates the pre-fork discovery service
	// if l.netCfg.PastAlanFork() {
	// 	l.closePreForkListener()
	//	return l.postForkListener.RandomNodes()
	// }

	preForkIterator := l.preForkListener.RandomNodes()
	postForkIterator := l.postForkListener.RandomNodes()
	return NewPreAndPostForkIterator(preForkIterator, postForkIterator)
}

// Returns all nodes from the pre and post-fork listeners
func (l *forkListener) AllNodes() []*enode.Node {
	// TODO: Uncomment the code below for the release that deprecates the pre-fork discovery service
	// if l.netCfg.PastAlanFork() {
	// 	l.closePreForkListener()
	//	return l.postForkListener.AllNodes()
	// }

	enodes := l.postForkListener.AllNodes()
	enodes = append(enodes, l.preForkListener.AllNodes()...)
	return enodes
}

// Sends a ping in the post-fork service.
// If it fails, try with the pre-fork service.
func (l *forkListener) Ping(node *enode.Node) error {
	// TODO: Uncomment the code below for the release that deprecates the pre-fork discovery service
	// if l.netCfg.PastAlanFork() {
	// 	l.closePreForkListener()
	//	return l.postForkListener.Ping(node)
	// }

	err := l.postForkListener.Ping(node)
	if err != nil {
		return l.preForkListener.Ping(node)
	}
	return err
}

// Returns the LocalNode using the post-fork listener.
// Both pre and post-fork listeners should have the same LocalNode
func (l *forkListener) LocalNode() *enode.LocalNode {
	// TODO: Uncomment the code below for the release that deprecates the pre-fork discovery service
	// if l.netCfg.PastAlanFork() {
	// 	l.closePreForkListener()
	//	return l.postForkListener.LocalNode()
	// }
	return l.postForkListener.LocalNode()
}

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
	} else {
		return i.postForkIterator.Node()
	}
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
