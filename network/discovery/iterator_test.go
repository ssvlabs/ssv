package discovery

import (
	"testing"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockIterator is a basic mock implementation of the enode.Iterator interface.
type MockIterator struct {
	nodes    []*enode.Node
	position int
	closed   bool
}

func NewMockIterator(nodes []*enode.Node) *MockIterator {
	return &MockIterator{
		nodes:    nodes,
		position: -1,
	}
}

func (m *MockIterator) Next() bool {
	if m.closed || m.position >= len(m.nodes)-1 {
		return false
	}
	m.position++
	return true
}

func (m *MockIterator) Node() *enode.Node {
	if m.closed || m.position == -1 || m.position >= len(m.nodes) {
		return nil
	}
	return m.nodes[m.position]
}

func (m *MockIterator) Close() {
	m.closed = true
}

// MockIdentityScheme for mocking enode.Node
type MockIdentityScheme struct {
	Addr [32]byte
}

func (is *MockIdentityScheme) Verify(r *enr.Record, sig []byte) error {
	return nil
}
func (is *MockIdentityScheme) NodeAddr(r *enr.Record) []byte {
	return is.Addr[:]
}

// Mock enode.Node
func NewTestingNode(t *testing.T, id byte) *enode.Node {
	node, err := enode.New(&MockIdentityScheme{Addr: [32]byte{id}}, &enr.Record{})
	require.NoError(t, err)
	return node
}

func TestPreAndPostForkIterator_Next(t *testing.T) {
	// Mock nodes
	node1 := NewTestingNode(t, 1)
	node2 := NewTestingNode(t, 2)
	node3 := NewTestingNode(t, 3)
	node4 := NewTestingNode(t, 4)

	// Mock iterators
	preFork := NewMockIterator([]*enode.Node{node3, node4})  // preFork has 2 nodes
	postFork := NewMockIterator([]*enode.Node{node1, node2}) // postFork has 2 nodes

	iterator := NewPreAndPostForkIterator(preFork, postFork)

	// First two calls should return nodes from postFork
	assert.True(t, iterator.Next()) // First postFork node
	assert.Equal(t, node1, iterator.Node())

	assert.True(t, iterator.Next()) // Second postFork node
	assert.Equal(t, node2, iterator.Node())

	// Then, switch to preFork
	assert.True(t, iterator.Next()) // First preFork node
	assert.Equal(t, node3, iterator.Node())

	assert.True(t, iterator.Next()) // Second preFork node
	assert.Equal(t, node4, iterator.Node())

	// No more elements
	assert.False(t, iterator.Next())
}

func TestPreAndPostForkIterator_PostForkEmpty(t *testing.T) {
	// Mock nodes
	node1 := NewTestingNode(t, 1)
	node2 := NewTestingNode(t, 2)

	// Mock iterators
	preFork := NewMockIterator([]*enode.Node{node2, node1}) // preFork has 2 nodes
	postFork := NewMockIterator([]*enode.Node{})            // postFork has no node

	iterator := NewPreAndPostForkIterator(preFork, postFork)

	// First check: preFork first node after switch
	assert.True(t, iterator.Next())
	assert.Equal(t, node2, iterator.Node())

	// Second check: preFork second node
	assert.True(t, iterator.Next())
	assert.Equal(t, node1, iterator.Node())

	// No more elements
	assert.False(t, iterator.Next())
}

func TestPreAndPostForkIterator_PreForkEmpty(t *testing.T) {
	// Mock nodes
	node1 := NewTestingNode(t, 1)
	node2 := NewTestingNode(t, 2)

	// Mock iterators
	preFork := NewMockIterator([]*enode.Node{})              // preFork has no node
	postFork := NewMockIterator([]*enode.Node{node1, node2}) // postFork has 2 nodes

	iterator := NewPreAndPostForkIterator(preFork, postFork)

	// First check: postFork first node
	assert.True(t, iterator.Next())
	assert.Equal(t, node1, iterator.Node())

	// Second check: postFork second node
	assert.True(t, iterator.Next())
	assert.Equal(t, node2, iterator.Node())

	// No more elements even after switch
	assert.False(t, iterator.Next())
}

func TestPreAndPostForkIterator_Close(t *testing.T) {
	// Mock iterators
	preFork := NewMockIterator(nil)
	postFork := NewMockIterator(nil)

	iterator := NewPreAndPostForkIterator(preFork, postFork)

	// No elements
	assert.False(t, iterator.Next())

	iterator.Close()

	// Check that both iterators are closed
	assert.True(t, preFork.closed)
	assert.True(t, postFork.closed)
}
