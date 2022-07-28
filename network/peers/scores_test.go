package peers

import (
	"github.com/bloxapp/ssv/network/commons"
	nettesting "github.com/bloxapp/ssv/network/testing"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestScoresIndex(t *testing.T) {
	nks, err := nettesting.CreateKeys(1)
	require.NoError(t, err)

	sk, err := commons.ConvertToInterfacePrivkey(nks[0].NetKey)
	require.NoError(t, err)
	pid, err := peer.IDFromPrivateKey(sk)
	require.NoError(t, err)

	si := newScoreIndex()

	require.NoError(t, si.Score(pid, &NodeScore{
		Name:  "decided",
		Value: 1.0,
	}, &NodeScore{
		Name:  "relays",
		Value: -2.0,
	}))

	scores, err := si.GetScore(pid, "decided", "relays", "dummy")
	require.NoError(t, err)
	require.Len(t, scores, 2)
}

func TestPeersTopScores(t *testing.T) {
	pids, err := createPeerIDs(50)
	require.NoError(t, err)
	peerScores := make(map[peer.ID]int)
	for i, pid := range pids {
		peerScores[pid] = i + 1
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
		sk, err := commons.GenNetworkKey()
		if err != nil {
			return nil, err
		}
		isk, err := commons.ConvertToInterfacePrivkey(sk)
		if err != nil {
			return nil, errors.Wrap(err, "could not convert to interface key")
		}
		pid, err := peer.IDFromPrivateKey(isk)
		if err != nil {
			return nil, err
		}
		res = append(res, pid)
	}
	return res, nil
}
