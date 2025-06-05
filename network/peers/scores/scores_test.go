package scores_test

import (
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/network/peers/scores"
)

func TestScoresIndex(t *testing.T) {
	si := scores.NewScoreIndex()
	pid := peer.ID("peer1")

	require.NoError(t, si.Score(pid, &scores.NodeScore{
		Name:  "decided",
		Value: 1.0,
	}, &scores.NodeScore{
		Name:  "relays",
		Value: -2.0,
	}))

	scores, err := si.GetScore(pid, "decided", "relays", "dummy")
	require.NoError(t, err)
	require.Len(t, scores, 2)
}

func TestPeersTopScores(t *testing.T) {
	pids := []peer.ID{
		peer.ID("peer1"),
		peer.ID("peer2"),
		peer.ID("peer3"),
	}
	peerScores := make(map[peer.ID]scores.PeerScore)
	for i, pid := range pids {
		peerScores[pid] = scores.PeerScore(i) + 1
	}
	top := scores.GetTopScores(peerScores, 2)
	require.Len(t, top, 2)
	_, ok := top[pids[0]]
	require.False(t, ok)
	_, ok = top[pids[2]]
	require.True(t, ok)
}
