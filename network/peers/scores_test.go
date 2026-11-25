package peers

import (
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/network/commons"
	nettesting "github.com/ssvlabs/ssv/network/testing"
)

func TestScoresIndex(t *testing.T) {
	nks, err := nettesting.CreateKeys(1)
	require.NoError(t, err)

	sk, err := commons.ECDSAPrivToInterface(nks[0].NetKey)
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

	si.RemovePeer(pid)
	scores, err = si.GetScore(pid, "decided", "relays")
	require.NoError(t, err)
	require.Empty(t, scores)
}

func TestPeersIndexScoreRequiresKnownPeer(t *testing.T) {
	pid := peer.ID("peer")
	peerInfos := NewPeerInfoIndex()
	scores := newScoreIndex()
	index := &peersIndex{PeerInfoIndex: peerInfos, SubnetsIndex: NewSubnetsIndex(), scoreIdx: scores}

	require.NoError(t, index.Score(pid, &NodeScore{Name: "validation", Value: 1}))
	stored, err := scores.GetScore(pid, "validation")
	require.NoError(t, err)
	require.Empty(t, stored)

	peerInfos.SetState(pid, StateConnected)
	require.NoError(t, index.Score(pid, &NodeScore{Name: "validation", Value: 1}))
	stored, err = scores.GetScore(pid, "validation")
	require.NoError(t, err)
	require.Equal(t, []NodeScore{{Name: "validation", Value: 1}}, stored)

	index.RemovePeerState(pid)
	require.NoError(t, index.Score(pid, &NodeScore{Name: "validation", Value: 2}))
	stored, err = scores.GetScore(pid, "validation")
	require.NoError(t, err)
	require.Empty(t, stored)
}
