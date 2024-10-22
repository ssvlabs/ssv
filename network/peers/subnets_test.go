package peers

import (
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/records"
	nettesting "github.com/ssvlabs/ssv/network/testing"
)

func TestSubnetsIndex(t *testing.T) {
	nks, err := nettesting.CreateKeys(4)
	require.NoError(t, err)

	var pids []peer.ID
	for _, nk := range nks {
		sk, err := commons.ECDSAPrivToInterface(nk.NetKey)
		require.NoError(t, err)
		pid, err := peer.IDFromPrivateKey(sk)
		require.NoError(t, err)
		pids = append(pids, pid)
	}

	sAll, err := records.Subnets{}.FromString("0xffffffffffffffffffffffffffffffff")
	require.NoError(t, err)
	sNone, err := records.Subnets{}.FromString("0x00000000000000000000000000000000")
	require.NoError(t, err)
	sPartial, err := records.Subnets{}.FromString("0x57b080fffd743d9878dc41a184ab160a")
	require.NoError(t, err)

	subnetsIdx := NewSubnetsIndex(128)

	initialMapping := map[peer.ID]records.Subnets{
		pids[0]: sAll,
		pids[1]: sNone,
		pids[2]: sPartial,
		pids[3]: sPartial,
	}

	for pid, subnets := range initialMapping {
		subnetsIdx.UpdatePeerSubnets(pid, subnets)
	}

	require.Len(t, subnetsIdx.GetSubnetPeers(0), 3)
	require.Len(t, subnetsIdx.GetSubnetPeers(10), 1)

	for _, pid := range pids {
		subnets, ok := subnetsIdx.GetPeerSubnets(pid)
		require.True(t, ok)
		require.Equal(t, initialMapping[pid], subnets)
	}

	subnetsIdx.UpdatePeerSubnets(pids[0], sPartial)

	require.Len(t, subnetsIdx.GetSubnetPeers(0), 3)
	require.Len(t, subnetsIdx.GetSubnetPeers(10), 0)

	stats := subnetsIdx.GetSubnetsStats()
	require.Equal(t, 3, stats.PeersCount[0])

	subnetsIdx.UpdatePeerSubnets(pids[0], sNone)
	subnetsIdx.UpdatePeerSubnets(pids[2], sNone)
	subnetsIdx.UpdatePeerSubnets(pids[3], sNone)

	require.Len(t, subnetsIdx.GetSubnetPeers(0), 0)
	require.Len(t, subnetsIdx.GetSubnetPeers(10), 0)
}

func TestSubnetsDistributionScores(t *testing.T) {
	mySubnets := records.Subnets{}
	for i := 0; i < commons.Subnets(); i++ {
		if i%2 != 0 {
			mySubnets[i] = byte(1)
		}
	}

	t.Logf("my subnets: %v", mySubnets.String())

	stats := &SubnetsStats{
		PeersCount: make([]int, len(mySubnets)),
		Connected:  make([]int, len(mySubnets)),
	}
	for sub := range mySubnets {
		stats.Connected[sub] = 1 + rand.Intn(20)
		stats.PeersCount[sub] = stats.Connected[sub] + rand.Intn(10)
	}
	stats.Connected[1] = 0
	stats.Connected[3] = 1
	stats.Connected[5] = 30
	stats.PeersCount[5] = 30

	distScores := GetSubnetsDistributionScores(stats, 3, mySubnets, 5)

	require.Len(t, distScores, len(mySubnets))
	require.Equal(t, float64(0), distScores[0])
	require.Equal(t, float64(4.2), distScores[1])
	require.Equal(t, float64(2.533333333333333), distScores[3])
	require.Equal(t, float64(-6.05), distScores[5])
}

func TestSubnetScore(t *testing.T) {
	testCases := []struct {
		connected int
		min       int
		max       int
		expected  float64
	}{
		{connected: 0, min: 5, max: 20, expected: 4.0},
		{connected: 2, min: 5, max: 20, expected: 2.2},
		{connected: 4, min: 5, max: 20, expected: 1.4},
		{connected: 5, min: 5, max: 20, expected: 1.0},
		{connected: 10, min: 5, max: 20, expected: 0.666667},
		{connected: 15, min: 5, max: 20, expected: 0.333333},
		{connected: 20, min: 5, max: 20, expected: 0.0},
		{connected: 25, min: 5, max: 20, expected: -0.166667},
		{connected: 50, min: 5, max: 20, expected: -1.0},
		{connected: 100, min: 5, max: 20, expected: -2.666667},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("Connected-%d Min-%d Max-%d", tc.connected, tc.min, tc.max), func(t *testing.T) {
			score := scoreSubnet(tc.connected, tc.min, tc.max)
			if math.Abs(score-tc.expected) > 1e-6 {
				t.Errorf("Expected score to be %f, got %f", tc.expected, score)
			}
		})
	}
}
