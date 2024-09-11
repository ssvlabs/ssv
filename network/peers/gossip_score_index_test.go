package peers

import (
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGetGossipSubScore(t *testing.T) {
	index := NewGossipSubScoreIndex()
	peerID := peer.ID("peer1")

	score, exists := index.GetGossipSubScore(peerID)
	require.False(t, exists)
	require.Equal(t, 0.0, score)

	index.AddScore(peerID, 10.0)
	score, exists = index.GetGossipSubScore(peerID)
	require.True(t, exists)
	require.Equal(t, 10.0, score)
}

func TestAddScore(t *testing.T) {
	index := NewGossipSubScoreIndex()
	peerID := peer.ID("peer1")

	index.AddScore(peerID, 10.0)
	score, exists := index.GetGossipSubScore(peerID)
	require.True(t, exists)
	require.Equal(t, 10.0, score)

	peerID2 := peer.ID("peer2")

	index.AddScore(peerID2, -100.0)
	score2, exists2 := index.GetGossipSubScore(peerID2)
	require.True(t, exists2)
	require.Equal(t, -100.0, score2)
}

func TestClear(t *testing.T) {
	index := NewGossipSubScoreIndex()
	peerID := peer.ID("peer1")

	index.AddScore(peerID, 10.0)
	index.Clear()
	score, exists := index.GetGossipSubScore(peerID)
	require.False(t, exists)
	require.Equal(t, 0.0, score)
}

func TestHasBadGossipSubScore(t *testing.T) {
	index := NewGossipSubScoreIndex()
	peerID := peer.ID("peer1")

	bad, score := index.HasBadGossipSubScore(peerID)
	require.False(t, bad)
	require.Equal(t, 0.0, score)

	index.AddScore(peerID, index.graylistThreshold-1)
	bad, score = index.HasBadGossipSubScore(peerID)
	require.True(t, bad)
	require.Equal(t, index.graylistThreshold-1, score)

	index.AddScore(peerID, index.graylistThreshold+1)
	bad, score = index.HasBadGossipSubScore(peerID)
	require.False(t, bad)
	require.Equal(t, index.graylistThreshold+1, score)
}
