package peers

import (
	"encoding/hex"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/network/commons"
	nettesting "github.com/ssvlabs/ssv/network/testing"
)

func TestSubnetsIndex(t *testing.T) {
	nks, err := nettesting.CreateKeys(4)
	require.NoError(t, err)

	pids := make([]peer.ID, 0, len(nks))
	for _, nk := range nks {
		sk, err := commons.ECDSAPrivToInterface(nk.NetKey)
		require.NoError(t, err)
		pid, err := peer.IDFromPrivateKey(sk)
		require.NoError(t, err)
		pids = append(pids, pid)
	}

	sPartial, err := commons.SubnetsFromString("0x57b080fffd743d9878dc41a184ab160a")
	require.NoError(t, err)

	subnetsIdx := NewSubnetsIndex()

	initialMapping := map[peer.ID]commons.Subnets{
		pids[0]: commons.AllSubnets,
		pids[1]: commons.ZeroSubnets,
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

	subnetsIdx.UpdatePeerSubnets(pids[0], commons.ZeroSubnets)
	subnetsIdx.UpdatePeerSubnets(pids[2], commons.ZeroSubnets)
	subnetsIdx.UpdatePeerSubnets(pids[3], commons.ZeroSubnets)

	require.Len(t, subnetsIdx.GetSubnetPeers(0), 0)
	require.Len(t, subnetsIdx.GetSubnetPeers(10), 0)
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
		s, err := commons.SubnetsFromString(subnetHex)
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
			subnetsIdx := NewSubnetsIndex()
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
