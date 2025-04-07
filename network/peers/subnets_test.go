package peers

import (
	"encoding/hex"
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/network/commons"
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

	sAll, err := commons.FromString("0xffffffffffffffffffffffffffffffff")
	require.NoError(t, err)
	sNone, err := commons.FromString("0x00000000000000000000000000000000")
	require.NoError(t, err)
	sPartial, err := commons.FromString("0x57b080fffd743d9878dc41a184ab160a")
	require.NoError(t, err)

	subnetsIdx := NewSubnetsIndex(128)

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
	mysubnets := make(commons.Subnets, nsubnets)
	allSubs, _ := commons.FromString(commons.AllSubnets)
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

func TestUpdatePeerSubnets_Removal(t *testing.T) {
	generateTestPeers := func(t *testing.T, count int) []peer.ID {
		nks, err := nettesting.CreateKeys(count)
		require.NoError(t, err)

		var pids []peer.ID
		for _, nk := range nks {
			sk, err := commons.ECDSAPrivToInterface(nk.NetKey)
			require.NoError(t, err)
			pid, err := peer.IDFromPrivateKey(sk)
			require.NoError(t, err)
			pids = append(pids, pid)
		}
		return pids
	}

	getSubnet := func(t *testing.T, subnetHex string) commons.Subnets {
		require.Len(t, subnetHex, 32, "subnetHex must be 32 characters long, got %d", len(subnetHex))
		s, err := commons.FromString(subnetHex)
		require.NoError(t, err)
		return s
	}

	generateSubnetString := func(subnetIndices ...int) string {
		bytes := make([]byte, 16)
		for _, idx := range subnetIndices {
			if idx < 0 || idx >= 128 {
				continue
			}
			byteIndex := idx / 8
			bitIndex := idx % 8
			bytes[byteIndex] |= 1 << bitIndex
		}
		return hex.EncodeToString(bytes)
	}

	tests := []struct {
		name            string
		numPeers        int
		peerToRemoveIdx int
		initialSubnets  string
		removeSubnets   string
		expectedPeers   []int
	}{
		{
			name:            "RemoveFirstPeer",
			numPeers:        3,
			peerToRemoveIdx: 0,
			initialSubnets:  generateSubnetString(0), // Subnet 0
			removeSubnets:   generateSubnetString(),  // No subnets
			expectedPeers:   []int{1, 2},
		},
		{
			name:            "RemoveMiddlePeer",
			numPeers:        4,
			peerToRemoveIdx: 2,
			initialSubnets:  generateSubnetString(0), // Subnet 0
			removeSubnets:   generateSubnetString(),  // No subnets
			expectedPeers:   []int{0, 1, 3},
		},
		{
			name:            "RemoveLastPeer",
			numPeers:        3,
			peerToRemoveIdx: 2,
			initialSubnets:  generateSubnetString(0), // Subnet 0
			removeSubnets:   generateSubnetString(),  // No subnets
			expectedPeers:   []int{0, 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subnetsIdx := NewSubnetsIndex(128)
			pids := generateTestPeers(t, tt.numPeers)
			subnetID := 0
			sInitial := getSubnet(t, tt.initialSubnets)
			sRemove := getSubnet(t, tt.removeSubnets)

			emptySubnet := generateSubnetString()
			if tt.initialSubnets != emptySubnet {
				for _, pid := range pids {
					updated := subnetsIdx.UpdatePeerSubnets(pid, sInitial)
					require.True(t, updated)
				}

				peersInSubnet := subnetsIdx.GetSubnetPeers(subnetID)
				require.ElementsMatch(t, pids, peersInSubnet)
			}

			var peerToRemove peer.ID
			if tt.peerToRemoveIdx < len(pids) {
				peerToRemove = pids[tt.peerToRemoveIdx]
			} else {
				extraPids := generateTestPeers(t, 1)
				peerToRemove = extraPids[0]
			}

			updated := subnetsIdx.UpdatePeerSubnets(peerToRemove, sRemove)
			require.True(t, updated)

			peersInSubnet := subnetsIdx.GetSubnetPeers(subnetID)
			expectedPeers := make([]peer.ID, len(tt.expectedPeers))
			for i, idx := range tt.expectedPeers {
				expectedPeers[i] = pids[idx]
			}
			require.ElementsMatch(t, expectedPeers, peersInSubnet)
		})
	}
}
