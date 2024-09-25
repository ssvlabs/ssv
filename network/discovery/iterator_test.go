package discovery

import (
	"testing"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/stretchr/testify/assert"
)

func TestPreAndPostForkIterator_Next(t *testing.T) {
	// Mock nodes
	node1 := NewTestingNode(t)
	node2 := NewTestingNode(t)
	node3 := NewTestingNode(t)
	node4 := NewTestingNode(t)

	// Mock iterators
	preFork := NewMockIterator([]*enode.Node{node3, node4})  // preFork has 2 nodes
	postFork := NewMockIterator([]*enode.Node{node1, node2}) // postFork has 2 nodes

	iterator := NewPreAndPostForkIterator(preFork, postFork)

	// First two calls should return nodes from postFork
	assert.True(t, iterator.Next()) // First postFork node
	assert.Equal(t, node1, iterator.Node())

	assert.True(t, iterator.Next()) // Second postFork node
	assert.Equal(t, node2, iterator.Node())

	assert.False(t, postFork.closed) // postFork iterator must be still openened

	// Then, switch to preFork
	assert.True(t, iterator.Next()) // First preFork node
	assert.True(t, postFork.closed) // postFork iterator must closed after transition to preFork
	assert.Equal(t, node3, iterator.Node())

	assert.True(t, iterator.Next()) // Second preFork node
	assert.Equal(t, node4, iterator.Node())

	// No more elements
	assert.False(t, iterator.Next())
}

func TestPreAndPostForkIterator_PostForkEmpty(t *testing.T) {
	// Mock nodes
	node1 := NewTestingNode(t)
	node2 := NewTestingNode(t)

	// Mock iterators
	preFork := NewMockIterator([]*enode.Node{node2, node1}) // preFork has 2 nodes
	postFork := NewMockIterator([]*enode.Node{})            // postFork has no node

	iterator := NewPreAndPostForkIterator(preFork, postFork)

	assert.False(t, postFork.closed) // postFork iterator must start openened

	// First check: preFork first node after switch
	assert.True(t, iterator.Next())
	assert.True(t, postFork.closed) // postFork iterator must closed after transition to preFork
	assert.Equal(t, node2, iterator.Node())

	// Second check: preFork second node
	assert.True(t, iterator.Next())
	assert.Equal(t, node1, iterator.Node())

	// No more elements
	assert.False(t, iterator.Next())
}

func TestPreAndPostForkIterator_PreForkEmpty(t *testing.T) {
	// Mock nodes
	node1 := NewTestingNode(t)
	node2 := NewTestingNode(t)

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

	assert.True(t, postFork.closed) // postFork iterator must closed after transition to preFork
}

func TestPreAndPostForkIterator_Close(t *testing.T) {
	// Mock iterators
	preFork := NewMockIterator(nil)
	postFork := NewMockIterator(nil)

	iterator := NewPreAndPostForkIterator(preFork, postFork)

	// Check that both iterators are opened
	assert.False(t, preFork.closed)
	assert.False(t, postFork.closed)

	// No elements
	assert.False(t, iterator.Next())

	iterator.Close()

	// Check that both iterators are closed
	assert.True(t, preFork.closed)
	assert.True(t, postFork.closed)
}
