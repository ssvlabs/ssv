package discovery

import (
	"testing"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/stretchr/testify/assert"
)

func TestForkListener_Create(t *testing.T) {
	localNode := NewLocalNode(t)

	preForkListener := NewMockListener(localNode, []*enode.Node{})
	postForkListener := NewMockListener(localNode, []*enode.Node{})

	t.Run("Pre-Fork", func(t *testing.T) {
		netCfg := PreForkNetworkConfig()
		_ = newForkListener(preForkListener, postForkListener, netCfg)

		assert.False(t, preForkListener.closed)
		assert.False(t, postForkListener.closed)
	})

	t.Run("Post-Fork", func(t *testing.T) {
		netCfg := PostForkNetworkConfig()
		_ = newForkListener(preForkListener, postForkListener, netCfg)

		assert.False(t, preForkListener.closed)
		assert.False(t, postForkListener.closed)
	})
}

func TestForkListener_Lookup(t *testing.T) {
	nodeFromPreForkListener := NewTestingNode(t)  // pre-fork node
	nodeFromPostForkListener := NewTestingNode(t) // post-fork node
	localNode := NewLocalNode(t)

	preForkListener := NewMockListener(localNode, []*enode.Node{nodeFromPreForkListener})
	postForkListener := NewMockListener(localNode, []*enode.Node{nodeFromPostForkListener})

	t.Run("Pre-Fork", func(t *testing.T) {
		netCfg := PreForkNetworkConfig()
		forkListener := newForkListener(preForkListener, postForkListener, netCfg)

		nodes := forkListener.Lookup(enode.ID{})
		assert.Len(t, nodes, 2)
		// post-fork nodes are set first
		assert.Equal(t, nodes[0], nodeFromPostForkListener)
		assert.Equal(t, nodes[1], nodeFromPreForkListener)
	})

	t.Run("Post-Fork", func(t *testing.T) {
		netCfg := PostForkNetworkConfig()
		forkListener := newForkListener(preForkListener, postForkListener, netCfg)

		nodes := forkListener.Lookup(enode.ID{})
		// only post-fork nodes
		assert.Len(t, nodes, 1)
		assert.Equal(t, nodes[0], nodeFromPostForkListener)
	})
}

func TestForkListener_RandomNodes(t *testing.T) {
	nodeFromPreForkListener := NewTestingNode(t)  // pre-fork node
	nodeFromPostForkListener := NewTestingNode(t) // post-fork node
	localNode := NewLocalNode(t)

	preForkListener := NewMockListener(localNode, []*enode.Node{nodeFromPreForkListener})
	postForkListener := NewMockListener(localNode, []*enode.Node{nodeFromPostForkListener})

	t.Run("Pre-Fork", func(t *testing.T) {
		netCfg := PreForkNetworkConfig()
		forkListener := newForkListener(preForkListener, postForkListener, netCfg)

		iter := forkListener.RandomNodes()
		var nodes []*enode.Node
		for iter.Next() {
			nodes = append(nodes, iter.Node())
		}
		iter.Close()

		assert.Len(t, nodes, 2)
		// post-fork nodes are set first
		assert.Equal(t, nodes[0], nodeFromPostForkListener)
		assert.Equal(t, nodes[1], nodeFromPreForkListener)
	})

	t.Run("Post-Fork", func(t *testing.T) {
		netCfg := PostForkNetworkConfig()
		forkListener := newForkListener(preForkListener, postForkListener, netCfg)

		iter := forkListener.RandomNodes()
		var nodes []*enode.Node
		for iter.Next() {
			nodes = append(nodes, iter.Node())
		}
		iter.Close()

		// only post-fork nodes
		assert.Len(t, nodes, 1)
		assert.Equal(t, nodes[0], nodeFromPostForkListener)
	})
}

func TestForkListener_AllNodes(t *testing.T) {
	nodeFromPreForkListener := NewTestingNode(t)  // pre-fork node
	nodeFromPostForkListener := NewTestingNode(t) // post-fork node
	localNode := NewLocalNode(t)

	preForkListener := NewMockListener(localNode, []*enode.Node{nodeFromPreForkListener})
	postForkListener := NewMockListener(localNode, []*enode.Node{nodeFromPostForkListener})

	t.Run("Pre-Fork", func(t *testing.T) {
		netCfg := PreForkNetworkConfig()
		forkListener := newForkListener(preForkListener, postForkListener, netCfg)

		nodes := forkListener.AllNodes()
		assert.Len(t, nodes, 2)
		// post-fork nodes are set first
		assert.Equal(t, nodes[0], nodeFromPostForkListener)
		assert.Equal(t, nodes[1], nodeFromPreForkListener)
	})

	t.Run("Post-Fork", func(t *testing.T) {
		netCfg := PostForkNetworkConfig()
		forkListener := newForkListener(preForkListener, postForkListener, netCfg)

		nodes := forkListener.AllNodes()
		// only post-fork nodes
		assert.Len(t, nodes, 1)
		assert.Equal(t, nodes[0], nodeFromPostForkListener)
	})
}

func TestForkListener_PingPreFork(t *testing.T) {
	pingPeer := NewTestingNode(t) // any peer to ping
	localNode := NewLocalNode(t)

	preForkListener := NewMockListener(localNode, []*enode.Node{})
	postForkListener := NewMockListener(localNode, []*enode.Node{})

	netCfg := PreForkNetworkConfig()
	forkListener := newForkListener(preForkListener, postForkListener, netCfg)

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

func TestForkListener_PingPostFork(t *testing.T) {
	pingPeer := NewTestingNode(t) // any peer to ping
	localNode := NewLocalNode(t)

	preForkListener := NewMockListener(localNode, []*enode.Node{})
	postForkListener := NewMockListener(localNode, []*enode.Node{})

	netCfg := PostForkNetworkConfig()
	forkListener := newForkListener(preForkListener, postForkListener, netCfg)

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

		// Pre-Fork would succeed but it's not called since we're on post-fork
		assert.ErrorContains(t, err, "failed ping")
	})
}

func TestForkListener_LocalNode(t *testing.T) {
	localNode := NewLocalNode(t)

	preForkListener := NewMockListener(localNode, []*enode.Node{})
	postForkListener := NewMockListener(localNode, []*enode.Node{})

	t.Run("Pre-Fork", func(t *testing.T) {
		netCfg := PreForkNetworkConfig()
		forkListener := newForkListener(preForkListener, postForkListener, netCfg)

		assert.Equal(t, localNode, forkListener.LocalNode())
	})

	t.Run("Post-Fork", func(t *testing.T) {
		netCfg := PostForkNetworkConfig()
		forkListener := newForkListener(preForkListener, postForkListener, netCfg)

		assert.Equal(t, localNode, forkListener.LocalNode())
	})
}

func TestForkListener_Close(t *testing.T) {

	t.Run("Pre-Fork", func(t *testing.T) {
		preForkListener := NewMockListener(&enode.LocalNode{}, []*enode.Node{})
		postForkListener := NewMockListener(&enode.LocalNode{}, []*enode.Node{})

		netCfg := PreForkNetworkConfig()
		forkListener := newForkListener(preForkListener, postForkListener, netCfg)

		// Call any method so that it will check whether to close the pre-fork listener
		_ = forkListener.AllNodes()

		assert.False(t, preForkListener.closed)
		assert.False(t, postForkListener.closed)

		// Close
		forkListener.Close()

		assert.True(t, preForkListener.closed)
		assert.True(t, postForkListener.closed)
	})

	t.Run("Post-Fork", func(t *testing.T) {
		preForkListener := NewMockListener(&enode.LocalNode{}, []*enode.Node{})
		postForkListener := NewMockListener(&enode.LocalNode{}, []*enode.Node{})

		netCfg := PostForkNetworkConfig()
		forkListener := newForkListener(preForkListener, postForkListener, netCfg)

		// Call any method so that it will check whether to close the pre-fork listener
		_ = forkListener.AllNodes()

		assert.True(t, preForkListener.closed) // pre-fork listener is closed
		assert.False(t, postForkListener.closed)

		// Close
		forkListener.Close()

		assert.True(t, preForkListener.closed)
		assert.True(t, postForkListener.closed)
	})
}
