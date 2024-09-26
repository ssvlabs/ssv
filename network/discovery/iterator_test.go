package discovery

import (
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/stretchr/testify/assert"
)

func TestPreAndPostForkIterator_Next(t *testing.T) {
	// Mock iterators
	preforkNodes := NewTestingNodes(t, 2)
	postForkNodes := NewTestingNodes(t, 2)
	preFork := NewMockIterator(preforkNodes)
	postFork := NewMockIterator(postForkNodes)

	iterator, err := NewPreAndPostForkIterator(preFork, postFork, 5)
	assert.NoError(t, err)

	for _, node := range postForkNodes {
		assert.True(t, iterator.Next())
		assert.Equal(t, node, iterator.Node())
	}

	for _, node := range preforkNodes {
		assert.True(t, iterator.Next())
		assert.Equal(t, node, iterator.Node())
	}

	// No more elements
	assert.False(t, iterator.Next())
}

func TestPreAndPostForkIterator_Next_RoundRobin(t *testing.T) {
	const preforkFrequency = 5

	// Mock iterators
	preforkNodes := NewTestingNodes(t, 20)
	postForkNodes := NewTestingNodes(t, 80)
	preFork := NewMockIterator(preforkNodes)
	postFork := NewMockIterator(postForkNodes)

	iterator, err := NewPreAndPostForkIterator(preFork, postFork, preforkFrequency)
	assert.NoError(t, err)

	i, j := 0, 0
	for cursor := 1; cursor <= 100; cursor++ {
		var expectedNode *enode.Node
		if cursor%preforkFrequency == 0 {
			expectedNode = preforkNodes[j]
			j++
		} else {
			expectedNode = postForkNodes[i]
			i++
		}
		assert.True(t, iterator.Next())
		if !assert.ObjectsAreEqual(expectedNode, iterator.Node()) {
			gotPrefork := !slices.Contains(preforkNodes, iterator.Node())
			t.Fatalf("Unexpected node at iteration %d. Expected %v, got %v (got pre-fork: %t)", cursor, expectedNode, iterator.Node(), gotPrefork)
		}
	}

	// No more elements
	assert.False(t, iterator.Next())
}

func TestPreAndPostForkIterator_Next_False(t *testing.T) {
	// Mock iterators
	// Nil means return false on Next()
	preforkNodes := []*enode.Node{NewTestingNode(t), nil, NewTestingNode(t)}
	postForkNodes := []*enode.Node{nil, NewTestingNode(t), NewTestingNode(t)}
	preFork := NewMockIterator(preforkNodes)
	postFork := NewMockIterator(postForkNodes)

	iterator, err := NewPreAndPostForkIterator(preFork, postFork, 2)
	assert.NoError(t, err)

	expectNextFrom := func(node *enode.Node) {
		assert.True(t, iterator.Next())
		assert.Equal(t, node, iterator.Node())
	}

	expectNextFrom(preforkNodes[0])  // Round is post, but nil so fallback to pre
	expectNextFrom(postForkNodes[1]) // Round is pre, but nil so fallback to post
	expectNextFrom(postForkNodes[2]) // Round is post
	expectNextFrom(preforkNodes[2])  // Round is pre

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

	iterator, err := NewPreAndPostForkIterator(preFork, postFork, 5)
	assert.NoError(t, err)

	assert.False(t, postFork.closed) // postFork iterator must start openened

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
	node1 := NewTestingNode(t)
	node2 := NewTestingNode(t)

	// Mock iterators
	preFork := NewMockIterator([]*enode.Node{})              // preFork has no node
	postFork := NewMockIterator([]*enode.Node{node1, node2}) // postFork has 2 nodes

	iterator, err := NewPreAndPostForkIterator(preFork, postFork, 5)
	assert.NoError(t, err)

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

	iterator, err := NewPreAndPostForkIterator(preFork, postFork, 5)
	assert.NoError(t, err)

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
