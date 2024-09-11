package peers

import (
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
)

func TestGetGossipScore(t *testing.T) {
	index := NewGossipScoreIndex()
	peerID := peer.ID("peer1")
	peerID2 := peer.ID("peer2")

	score, exists := index.GetGossipScore(peerID)
	require.False(t, exists)
	require.Equal(t, 0.0, score)

	score, exists = index.GetGossipScore(peerID2)
	require.False(t, exists)
	require.Equal(t, 0.0, score)

	index.SetScores(map[peer.ID]float64{
		peerID: 10.0,
	})
	score, exists = index.GetGossipScore(peerID)
	require.True(t, exists)
	require.Equal(t, 10.0, score)

	score, exists = index.GetGossipScore(peerID2)
	require.False(t, exists)
	require.Equal(t, 0.0, score)
}

func TestSetScores(t *testing.T) {
	index := NewGossipScoreIndex()
	peerID := peer.ID("peer1")
	peerID2 := peer.ID("peer2")
	peerID3 := peer.ID("peer3")

	index.SetScores(map[peer.ID]float64{
		peerID:  10.0,
		peerID2: -100.0,
	})

	score, exists := index.GetGossipScore(peerID)
	require.True(t, exists)
	require.Equal(t, 10.0, score)

	score2, exists2 := index.GetGossipScore(peerID2)
	require.True(t, exists2)
	require.Equal(t, -100.0, score2)

	score3, exists3 := index.GetGossipScore(peerID3)
	require.False(t, exists3)
	require.Equal(t, 0.0, score3)
}

func TestClear(t *testing.T) {
	index := NewGossipScoreIndex()
	peerID := peer.ID("peer1")

	index.SetScores(map[peer.ID]float64{
		peerID: 10.0,
	})
	index.clear()
	score, exists := index.GetGossipScore(peerID)
	require.False(t, exists)
	require.Equal(t, 0.0, score)
}

func TestHasBadGossipScore(t *testing.T) {
	index := NewGossipScoreIndex()
	peerID := peer.ID("peer1")

	bad, score := index.HasBadGossipScore(peerID)
	require.False(t, bad)
	require.Equal(t, 0.0, score)

	index.SetScores(map[peer.ID]float64{
		peerID: index.graylistThreshold - 1,
	})
	bad, score = index.HasBadGossipScore(peerID)
	require.True(t, bad)
	require.Equal(t, index.graylistThreshold-1, score)

	index.SetScores(map[peer.ID]float64{
		peerID: index.graylistThreshold + 1,
	})
	bad, score = index.HasBadGossipScore(peerID)
	require.False(t, bad)
	require.Equal(t, index.graylistThreshold+1, score)
}
