package peers

import (
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/network/commons"
)

func TestPeerInfoIndexRemove(t *testing.T) {
	index := NewPeerInfoIndex()
	pid := peer.ID("peer")
	index.SetState(pid, StateConnected)

	index.Remove(pid)

	require.Nil(t, index.PeerInfo(pid))
	require.Equal(t, StateUnknown, index.State(pid))
}

func TestPeersIndexRemovePeer(t *testing.T) {
	pid := peer.ID("peer")
	peerInfos := NewPeerInfoIndex()
	peerInfos.SetState(pid, StateConnected)
	subnets := NewSubnetsIndex()
	peerSubnets := commons.ZeroSubnets
	peerSubnets.Set(1)
	subnets.UpdatePeerSubnets(pid, peerSubnets)
	scores := newScoreIndex()
	require.NoError(t, scores.Score(pid, &NodeScore{Name: "test", Value: 1}))
	index := &peersIndex{PeerInfoIndex: peerInfos, SubnetsIndex: subnets, scoreIdx: scores}

	index.RemovePeerState(pid)

	require.Nil(t, peerInfos.PeerInfo(pid))
	_, ok := subnets.GetPeerSubnets(pid)
	require.False(t, ok)
	storedScores, err := scores.GetScore(pid, "test")
	require.NoError(t, err)
	require.Empty(t, storedScores)
}

// RemovePeer on the full index is the subnets index's method: it must leave peer info and scores alone, because
// discovery holds the index as a SubnetsIndex and sees peers that are connected.
func TestPeersIndexRemovePeerRemovesSubnetsOnly(t *testing.T) {
	pid := peer.ID("peer")
	peerInfos := NewPeerInfoIndex()
	peerInfos.SetState(pid, StateConnected)
	subnets := NewSubnetsIndex()
	peerSubnets := commons.ZeroSubnets
	peerSubnets.Set(1)
	subnets.UpdatePeerSubnets(pid, peerSubnets)
	scores := newScoreIndex()
	require.NoError(t, scores.Score(pid, &NodeScore{Name: "test", Value: 1}))
	var index SubnetsIndex = &peersIndex{PeerInfoIndex: peerInfos, SubnetsIndex: subnets, scoreIdx: scores}

	index.RemovePeer(pid)

	_, ok := subnets.GetPeerSubnets(pid)
	require.False(t, ok)
	require.Equal(t, StateConnected, peerInfos.State(pid))
	storedScores, err := scores.GetScore(pid, "test")
	require.NoError(t, err)
	require.Len(t, storedScores, 1)
}
