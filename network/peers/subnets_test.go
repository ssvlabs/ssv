package peers

import (
	"github.com/bloxapp/ssv/network/records"
	nettesting "github.com/bloxapp/ssv/network/testing"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/stretchr/testify/require"
	"math/rand"
	"testing"
)

func TestSubnetsIndex(t *testing.T) {
	nks, err := nettesting.CreateKeys(4)
	require.NoError(t, err)

	var pids []peer.ID
	for _, nk := range nks {
		pid, err := peer.IDFromPrivateKey(crypto.PrivKey((*crypto.Secp256k1PrivateKey)(nk.NetKey)))
		require.NoError(t, err)
		pids = append(pids, pid)
	}

	sAll, err := records.Subnets{}.FromString("0xffffffffffffffffffffffffffffffff")
	require.NoError(t, err)
	sNone, err := records.Subnets{}.FromString("0x00000000000000000000000000000000")
	require.NoError(t, err)
	sPartial, err := records.Subnets{}.FromString("0x57b080fffd743d9878dc41a184ab160a")
	require.NoError(t, err)

	subnetsIdx := newSubnetsIndex(128)

	subnetsIdx.UpdatePeerSubnets(pids[0], sAll.Clone())
	subnetsIdx.UpdatePeerSubnets(pids[1], sNone.Clone())
	subnetsIdx.UpdatePeerSubnets(pids[2], sPartial.Clone())
	subnetsIdx.UpdatePeerSubnets(pids[3], sPartial.Clone())

	require.Len(t, subnetsIdx.GetSubnetPeers(0), 3)
	require.Len(t, subnetsIdx.GetSubnetPeers(10), 1)

	subnetsIdx.UpdatePeerSubnets(pids[0], sPartial.Clone())

	require.Len(t, subnetsIdx.GetSubnetPeers(0), 3)
	require.Len(t, subnetsIdx.GetSubnetPeers(10), 0)

	stats := subnetsIdx.GetSubnetsStats()
	require.Equal(t, 3, stats.PeersCount[0])

	subnetsIdx.UpdatePeerSubnets(pids[0], sNone.Clone())
	subnetsIdx.UpdatePeerSubnets(pids[2], sNone.Clone())
	subnetsIdx.UpdatePeerSubnets(pids[3], sNone.Clone())

	require.Len(t, subnetsIdx.GetSubnetPeers(0), 0)
	require.Len(t, subnetsIdx.GetSubnetPeers(10), 0)
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
	stats := &SubnetsStats{
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

	distScores := GetSubnetsDistributionScores(stats, 3, mysubnets, 5)

	require.Len(t, distScores, len(mysubnets))
	require.Equal(t, 0, distScores[0])
	require.Equal(t, 2, distScores[1])
	require.Equal(t, 1, distScores[3])
	require.Equal(t, -1, distScores[5])
}
