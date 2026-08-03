package discovery

import (
	"context"
	"time"

	"github.com/ethereum/go-ethereum/p2p/discover/v5wire"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// forkingDV5Listener wraps a pre-fork and a post-fork listener and queries both
// on every operation.
//
// The naming is historical. The pre-fork listener was meant to be dropped once
// the protocol-ID fork completed, but #1774 removed that shutdown deliberately
// and it has run permanently since. There is no fork check anywhere here, so
// both listeners stay live for the lifetime of the service.
//
// It still earns its keep: the pre-fork listener sets no protocol ID, so it is
// the only way to reach nodes that predate the fork. That also makes it the
// path by which unrelated discv5 traffic enters the node — see SharedUDPConn.
type forkingDV5Listener struct {
	logger           *zap.Logger
	preForkListener  Listener
	postForkListener Listener
	iteratorTimeout  time.Duration
}

func NewForkingDV5Listener(logger *zap.Logger, preFork, postFork Listener, iteratorTimeout time.Duration) *forkingDV5Listener {
	return &forkingDV5Listener{
		logger:           logger,
		preForkListener:  preFork,
		postForkListener: postFork,
		iteratorTimeout:  iteratorTimeout,
	}
}

// Lookup returns the combined result of a lookup in both listeners.
func (l *forkingDV5Listener) Lookup(id enode.ID) []*enode.Node {
	nodes := l.postForkListener.Lookup(id)
	nodes = append(nodes, l.preForkListener.Lookup(id)...)
	return nodes
}

// RandomNodes returns an iterator that draws fairly from both listeners.
func (l *forkingDV5Listener) RandomNodes() enode.Iterator {
	fairMix := enode.NewFairMix(l.iteratorTimeout)
	fairMix.AddSource(&annotatedIterator{l.postForkListener.RandomNodes(), "post"})
	fairMix.AddSource(&annotatedIterator{l.preForkListener.RandomNodes(), "pre"})
	return fairMix
}

// AllNodes returns the nodes held by both listeners.
func (l *forkingDV5Listener) AllNodes() []*enode.Node {
	enodes := l.postForkListener.AllNodes()
	enodes = append(enodes, l.preForkListener.AllNodes()...)
	return enodes
}

// Ping tries the post-fork listener first, falling back to the pre-fork one so
// that nodes reachable only on the default protocol ID still answer.
func (l *forkingDV5Listener) Ping(node *enode.Node) (*v5wire.Pong, error) {
	pong, err := l.postForkListener.Ping(node)
	if err != nil {
		return l.preForkListener.Ping(node)
	}
	return pong, nil
}

// Returns the LocalNode using the post-fork listener.
// Both pre and post-fork listeners should have the same LocalNode.
func (l *forkingDV5Listener) LocalNode() *enode.LocalNode {
	return l.postForkListener.LocalNode()
}

// Closes both listeners
func (l *forkingDV5Listener) Close() {
	l.closePreForkListener()
	l.postForkListener.Close()
}

// closePreForkListener ensures preForkListener is closed once
func (l *forkingDV5Listener) closePreForkListener() {
	l.preForkListener.Close()
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
	peerDiscoveryIterationsCounter.Add(
		context.TODO(), 1, metric.WithAttributes(attribute.String("ssv.fork", i.fork)))
	return true
}
