package p2pv1

import (
	"github.com/bloxapp/ssv/network/commons"
	forksfactory "github.com/bloxapp/ssv/network/forks/factory"
	"github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/network/records"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/stretchr/testify/require"
	"math/rand"
	"testing"
)

func TestGetMaxPeers(t *testing.T) {
	n := &p2pNetwork{
		cfg:  &Config{MaxPeers: 40, TopicMaxPeers: 8},
		fork: forksfactory.NewFork(forksprotocol.V2ForkVersion),
	}

	require.Equal(t, 40, n.getMaxPeers(""))
	require.Equal(t, 8, n.getMaxPeers("100"))
	require.Equal(t, 16, n.getMaxPeers(n.fork.DecidedTopic()))
}

func TestSubnetsDistributionScores(t *testing.T) {
	nsubnets := 128
	mysubnets := make(records.Subnets, nsubnets)
	allSubs, _ := records.Subnets{}.FromString(records.AllSubnets)
	for sub := range allSubs {
		if sub%2 == 0 {
			mysubnets[sub] = byte(0)
		} else {
			mysubnets[sub] = byte(1)
		}
	}
	stats := &peers.SubnetsStats{
		PeersCount: make([]int, len(mysubnets)),
		Connected:  make([]int, len(mysubnets)),
	}
	for sub := range mysubnets {
		stats.Connected[sub] = 1 + rand.Intn(20)
		stats.PeersCount[sub] = stats.Connected[sub] + rand.Intn(10)
	}
	stats.Connected[1] = 0
	stats.Connected[3] = 1
	stats.Connected[5] = 30
	stats.PeersCount[5] = 30

	distScores := getSubnetsDistributionScores(stats, 3, mysubnets, 5)

	require.Len(t, distScores, len(mysubnets))
	require.Equal(t, 0, distScores[0])
	require.Equal(t, 2, distScores[1])
	require.Equal(t, 1, distScores[3])
	require.Equal(t, -1, distScores[5])
}

func TestPeersTopScores(t *testing.T) {
	pids := createPeerIDs(50)
	peerScores := make(map[peer.ID]int)
	for i, pid := range pids {
		peerScores[pid] = i + 1
	}
	top := getTopScores(peerScores, 25)
	require.Len(t, top, 25)
	_, ok := top[pids[0]]
	require.False(t, ok)
	_, ok = top[pids[45]]
	require.True(t, ok)
}

func createPeerIDs(n int) []peer.ID {
	var res []peer.ID
	for len(res) < n {
		sk, _ := commons.GenNetworkKey()
		isk := crypto.PrivKey((*crypto.Secp256k1PrivateKey)(sk))
		pid, _ := peer.IDFromPrivateKey(isk)
		res = append(res, pid)
	}
	return res
}
