package discovery

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/stretchr/testify/require"
)

func TestFairMixIterator_Next(t *testing.T) {
	// Mock iterators
	preforkNodes := NewTestingNodes(t, 2)
	postForkNodes := NewTestingNodes(t, 2)

	iterator := enode.NewFairMix(5 * time.Millisecond)
	defer iterator.Close()
	iterator.AddSource(NewMockIterator(preforkNodes))
	iterator.AddSource(NewMockIterator(postForkNodes))

	expectedNodes := append(preforkNodes, postForkNodes...)
	actualNodes := make([]*enode.Node, 0, len(expectedNodes))
	for i := 0; i < 4; i++ {
		require.True(t, iterator.Next())
		actualNodes = append(actualNodes, iterator.Node())
	}
	require.ElementsMatch(t, expectedNodes, actualNodes)

	// No more elements
	requireNextTimeout(t, false, iterator, 15*time.Millisecond)
}

func TestFairMixIterator_Next_False(t *testing.T) {
	// Mock iterators
	// Nil means return false on Next()
	preforkNodes := []*enode.Node{NewTestingNode(t), nil, NewTestingNode(t)}
	postForkNodes := []*enode.Node{NewTestingNode(t), NewTestingNode(t), nil}
	preFork := NewMockIterator(preforkNodes)
	postFork := NewMockIterator(postForkNodes)

	iterator := enode.NewFairMix(5 * time.Millisecond)
	defer iterator.Close()
	iterator.AddSource(preFork)
	iterator.AddSource(postFork)

	expectedNodes := make([]*enode.Node, 0, len(preforkNodes)+len(postForkNodes))
	for _, node := range preforkNodes {
		if node == nil {
			break
		}
		expectedNodes = append(expectedNodes, node)
	}
	for _, node := range postForkNodes {
		if node == nil {
			break
		}
		expectedNodes = append(expectedNodes, node)
	}

	var actualNodes []*enode.Node
	for i := 0; i < len(expectedNodes); i++ {
		requireNextTimeout(t, true, iterator, 10*time.Millisecond)
		actualNodes = append(actualNodes, iterator.Node())
	}
	require.ElementsMatch(t, expectedNodes, actualNodes)

	// No more elements
	requireNextTimeout(t, false, iterator, 15*time.Millisecond)
}

func TestFairMixIterator_PostForkEmpty(t *testing.T) {
	// Mock nodes
	node1 := NewTestingNode(t)
	node2 := NewTestingNode(t)

	// Mock iterators
	preFork := NewMockIterator([]*enode.Node{node2, node1}) // preFork has 2 nodes
	postFork := NewMockIterator([]*enode.Node{})            // postFork has no node

	iterator := enode.NewFairMix(5 * time.Millisecond)
	defer iterator.Close()
	iterator.AddSource(preFork)
	iterator.AddSource(postFork)

	require.False(t, postFork.closed) // postFork iterator must start openened

	// First check: preFork first node after switch
	requireNextTimeout(t, true, iterator, 10*time.Millisecond)
	require.Equal(t, node2, iterator.Node())

	// Second check: preFork second node
	requireNextTimeout(t, true, iterator, 10*time.Millisecond)
	require.Equal(t, node1, iterator.Node())

	// No more elements
	requireNextTimeout(t, false, iterator, 15*time.Millisecond)
}

func TestFairMixIterator_PreForkEmpty(t *testing.T) {
	// Mock nodes
	node1 := NewTestingNode(t)
	node2 := NewTestingNode(t)

	// Mock iterators
	preFork := NewMockIterator([]*enode.Node{})              // preFork has no node
	postFork := NewMockIterator([]*enode.Node{node1, node2}) // postFork has 2 nodes

	iterator := enode.NewFairMix(5 * time.Millisecond)
	defer iterator.Close()
	iterator.AddSource(preFork)
	iterator.AddSource(postFork)

	// First check: postFork first node
	requireNextTimeout(t, true, iterator, 10*time.Millisecond)
	require.Equal(t, node1, iterator.Node())

	// Second check: postFork second node
	requireNextTimeout(t, true, iterator, 10*time.Millisecond)
	require.Equal(t, node2, iterator.Node())

	// No more elements even after switch
	requireNextTimeout(t, false, iterator, 15*time.Millisecond)
}
