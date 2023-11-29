package peers

import (
	crand "crypto/rand"
	"testing"

	"github.com/bloxapp/ssv/network/commons"
	nettesting "github.com/bloxapp/ssv/network/testing"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
)

func TestScoresIndex(t *testing.T) {
	nks, err := nettesting.CreateKeys(1)
	require.NoError(t, err)

	sk, err := commons.ECDSAPrivToInterface(nks[0].NetKey)
	require.NoError(t, err)
	pid, err := peer.IDFromPrivateKey(sk)
	require.NoError(t, err)

	si := newScoreIndex()
	score := -2.0
	si.Score(pid, score)

	scores, err := si.GetScore(pid)
	require.NoError(t, err)
	require.Equal(t, scores, score)
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
