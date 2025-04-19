package discovery

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const iteratorTimeout = 5 * time.Millisecond

func TestForkListener_Create(t *testing.T) {
	localNode := NewLocalNode(t)

	preForkListener := NewMockListener(localNode, []*enode.Node{})
	postForkListener := NewMockListener(localNode, []*enode.Node{})

	_ = NewForkingDV5Listener(zap.NewNop(), preForkListener, postForkListener, iteratorTimeout)

	assert.False(t, preForkListener.closed)
	assert.False(t, postForkListener.closed)
}

func TestForkListener_Lookup(t *testing.T) {
	nodeFromPreForkListener := NewTestingNode(t)  // pre-fork node
	nodeFromPostForkListener := NewTestingNode(t) // post-fork node
	localNode := NewLocalNode(t)

	preForkListener := NewMockListener(localNode, []*enode.Node{nodeFromPreForkListener})
	postForkListener := NewMockListener(localNode, []*enode.Node{nodeFromPostForkListener})

	forkListener := NewForkingDV5Listener(zap.NewNop(), preForkListener, postForkListener, iteratorTimeout)

	nodes := forkListener.Lookup(enode.ID{})
	assert.Len(t, nodes, 2)
	// post-fork nodes are set first
	assert.Equal(t, nodes[0], nodeFromPostForkListener)
	assert.Equal(t, nodes[1], nodeFromPreForkListener)
}

func TestForkListener_RandomNodes(t *testing.T) {
	nodeFromPreForkListener := NewTestingNode(t)  // pre-fork node
	nodeFromPostForkListener := NewTestingNode(t) // post-fork node
	localNode := NewLocalNode(t)

	preForkListener := NewMockListener(localNode, []*enode.Node{nodeFromPreForkListener})
	postForkListener := NewMockListener(localNode, []*enode.Node{nodeFromPostForkListener})

	forkListener := NewForkingDV5Listener(zap.NewNop(), preForkListener, postForkListener, iteratorTimeout)

	iter := forkListener.RandomNodes()
	defer iter.Close()
	var nodes []*enode.Node
	for i := 0; i < 2; i++ {
		require.True(t, iter.Next())
		nodes = append(nodes, iter.Node())
	}

	assert.Len(t, nodes, 2)
	// post-fork nodes are set first
	assert.Equal(t, nodes[0], nodeFromPreForkListener)
	assert.Equal(t, nodes[1], nodeFromPostForkListener)

	// No more next
	requireNextTimeout(t, false, iter, 10*time.Millisecond)
}

func TestForkListener_AllNodes(t *testing.T) {
	nodeFromPreForkListener := NewTestingNode(t)  // pre-fork node
	nodeFromPostForkListener := NewTestingNode(t) // post-fork node
	localNode := NewLocalNode(t)

	preForkListener := NewMockListener(localNode, []*enode.Node{nodeFromPreForkListener})
	postForkListener := NewMockListener(localNode, []*enode.Node{nodeFromPostForkListener})

	forkListener := NewForkingDV5Listener(zap.NewNop(), preForkListener, postForkListener, iteratorTimeout)

	nodes := forkListener.AllNodes()
	assert.Len(t, nodes, 2)
	// post-fork nodes are set first
	assert.Equal(t, nodes[0], nodeFromPostForkListener)
	assert.Equal(t, nodes[1], nodeFromPreForkListener)
}

func TestForkListener_PingPreFork(t *testing.T) {
	pingPeer := NewTestingNode(t) // any peer to ping
	localNode := NewLocalNode(t)

	preForkListener := NewMockListener(localNode, []*enode.Node{})
	postForkListener := NewMockListener(localNode, []*enode.Node{})

	forkListener := NewForkingDV5Listener(zap.NewNop(), preForkListener, postForkListener, iteratorTimeout)

	t.Run("Post-Fork succeeds", func(t *testing.T) {
		postForkListener.SetNodesForPingError([]*enode.Node{})
		preForkListener.SetNodesForPingError([]*enode.Node{pingPeer})
		err := forkListener.Ping(pingPeer)
		assert.NoError(t, err)
	})

	t.Run("Post-Fork fails and Pre-Fork succeeds", func(t *testing.T) {
		postForkListener.SetNodesForPingError([]*enode.Node{pingPeer})
		preForkListener.SetNodesForPingError([]*enode.Node{})
		err := forkListener.Ping(pingPeer)
		assert.NoError(t, err)
	})

	t.Run("Post-Fork and Pre-Fork fails", func(t *testing.T) {
		postForkListener.SetNodesForPingError([]*enode.Node{pingPeer})
		preForkListener.SetNodesForPingError([]*enode.Node{pingPeer})
		err := forkListener.Ping(pingPeer)
		assert.ErrorContains(t, err, "failed ping")
	})
}

func TestForkListener_LocalNode(t *testing.T) {
	localNode := NewLocalNode(t)

	preForkListener := NewMockListener(localNode, []*enode.Node{})
	postForkListener := NewMockListener(localNode, []*enode.Node{})

	forkListener := NewForkingDV5Listener(zap.NewNop(), preForkListener, postForkListener, iteratorTimeout)

	assert.Equal(t, localNode, forkListener.LocalNode())
}

func TestForkListener_Close(t *testing.T) {
	preForkListener := NewMockListener(&enode.LocalNode{}, []*enode.Node{})
	postForkListener := NewMockListener(&enode.LocalNode{}, []*enode.Node{})

	forkListener := NewForkingDV5Listener(zap.NewNop(), preForkListener, postForkListener, iteratorTimeout)

	// Call any method so that it will check whether to close the pre-fork listener
	_ = forkListener.AllNodes()

	assert.False(t, preForkListener.closed)
	assert.False(t, postForkListener.closed)

	// Close
	forkListener.Close()

	assert.True(t, preForkListener.closed)
	assert.True(t, postForkListener.closed)
}

func requireNextTimeout(t *testing.T, expected bool, iter enode.Iterator, timeout time.Duration) {
	const maxTries = 10
	var deadline = time.After(timeout)
	next := make(chan bool)
	go func() {
		defer close(next)
		for {
			ok := iter.Next()
			select {
			case next <- ok:
			case <-deadline:
				return
			}
			if ok {
				return
			}
			time.Sleep(timeout / maxTries)
		}
	}()
	for {
		select {
		case ok := <-next:
			require.Equal(t, expected, ok, "expected next to be %v", expected)
			if ok {
				return
			}
		case <-deadline:
			if expected {
				require.Fail(t, "expected next to be %v", expected)
			}
			return
		}
	}
}
