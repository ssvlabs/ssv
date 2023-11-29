package peers

import (
	crand "crypto/rand"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
)

func TestScoresIndex(t *testing.T) {
	pid, err := createRandomPeerID()
	require.NoError(t, err)

	si := newScoreIndex()
	originalScore := -2.0
	si.Score(pid, originalScore)

	score, err := si.GetScore(pid)
	require.NoError(t, err)
	require.Equal(t, score, originalScore)
}

func TestPeersTopScores(t *testing.T) {
	pids, err := createPeerIDs(50)
	require.NoError(t, err)
	peerScores := make(map[peer.ID]PeerScore)
	for i, pid := range pids {
		peerScores[pid] = PeerScore(i) + 1
	}
	top := GetTopScores(peerScores, 25)
	require.Len(t, top, 25)
	_, ok := top[pids[0]]
	require.False(t, ok)
	_, ok = top[pids[45]]
	require.True(t, ok)
}

func createPeerIDs(n int) ([]peer.ID, error) {
	var res []peer.ID
	for len(res) < n {
		isk, _, err := crypto.GenerateSecp256k1Key(crand.Reader)
		if err != nil {
			return nil, err
		}
		pid, err := peer.IDFromPrivateKey(isk)
		if err != nil {
			return nil, err
		}
		res = append(res, pid)
	}
	return res, nil
}

func createRandomPeerID() (peer.ID, error) {
	priv, _, err := crypto.GenerateSecp256k1Key(crand.Reader)
	if err != nil {
		return "", err
	}
	return peer.IDFromPrivateKey(priv)
}

func TestThresholdFunctionality(t *testing.T) {
	si := newScoreIndex()
	threshold := 5.0
	si.SetThreshold(threshold)

	pid1, _ := createRandomPeerID()
	pid2, _ := createRandomPeerID()
	si.Score(pid1, 4.0) // Below threshold
	si.Score(pid2, 6.0) // Above threshold

	require.True(t, si.IsBelowThreshold(pid1))
	require.False(t, si.IsBelowThreshold(pid2))
}

func TestGetScoreErrorHandling(t *testing.T) {
	si := newScoreIndex()
	randomPID, _ := createRandomPeerID()

	_, err := si.GetScore(randomPID)
	require.Error(t, err)
}

func TestGetTopScoresBoundaryCases(t *testing.T) {
	peerScores := make(map[peer.ID]PeerScore)
	pids, _ := createPeerIDs(10)

	for i, pid := range pids {
		peerScores[pid] = PeerScore(i)
	}

	// Requesting more scores than available
	top := GetTopScores(peerScores, 20)
	require.Len(t, top, 10)

	// Empty map
	top = GetTopScores(make(map[peer.ID]PeerScore), 5)
	require.Empty(t, top)
}
