package p2pv1

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/network/commons"
)

// createSubnets creates a commons.Subnets with the specified subnets active
func createSubnets(activeSubnets ...int) commons.Subnets {
	subnets := make(commons.Subnets, commons.SubnetsCount)
	for _, subnet := range activeSubnets {
		if subnet >= 0 && subnet < commons.SubnetsCount {
			subnets[subnet] = 1
		}
	}
	return subnets
}

// createSubnetPeers creates a SubnetPeers with the specified number of peers for each subnet
func createSubnetPeers(peerCounts map[int]uint16) SubnetPeers {
	var peers SubnetPeers
	for subnet, count := range peerCounts {
		if subnet >= 0 && subnet < commons.SubnetsCount {
			peers[subnet] = count
		}
	}
	return peers
}

func TestSubnetPeers_Add(t *testing.T) {
	tests := []struct {
		name     string
		a        SubnetPeers
		b        SubnetPeers
		expected SubnetPeers
	}{
		{
			name:     "empty subnets",
			a:        SubnetPeers{},
			b:        SubnetPeers{},
			expected: SubnetPeers{},
		},
		{
			name:     "one subnet in a",
			a:        createSubnetPeers(map[int]uint16{5: 3}),
			b:        SubnetPeers{},
			expected: createSubnetPeers(map[int]uint16{5: 3}),
		},
		{
			name:     "one subnet in b",
			a:        SubnetPeers{},
			b:        createSubnetPeers(map[int]uint16{10: 2}),
			expected: createSubnetPeers(map[int]uint16{10: 2}),
		},
		{
			name:     "different subnets",
			a:        createSubnetPeers(map[int]uint16{5: 3}),
			b:        createSubnetPeers(map[int]uint16{10: 2}),
			expected: createSubnetPeers(map[int]uint16{5: 3, 10: 2}),
		},
		{
			name:     "overlapping subnets",
			a:        createSubnetPeers(map[int]uint16{5: 3, 10: 1}),
			b:        createSubnetPeers(map[int]uint16{5: 2, 15: 4}),
			expected: createSubnetPeers(map[int]uint16{5: 5, 10: 1, 15: 4}),
		},
		{
			name:     "multiple subnets",
			a:        createSubnetPeers(map[int]uint16{1: 1, 2: 2, 3: 3}),
			b:        createSubnetPeers(map[int]uint16{3: 3, 4: 4, 5: 5}),
			expected: createSubnetPeers(map[int]uint16{1: 1, 2: 2, 3: 6, 4: 4, 5: 5}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.a.Add(tt.b)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestSubnetPeers_Score_DeadSubnets(t *testing.T) {
	// Test case where we have dead subnets (0 peers)
	// Dead subnets should have the highest priority (90)

	// Our node is subscribed to subnets 1, 2, 3
	ourSubnets := createSubnets(1, 2, 3)

	// We have 0 peers in subnet 1 (dead), 1 peer in subnet 2 (solo), 3 peers in subnet 3
	ourSubnetPeers := createSubnetPeers(map[int]uint16{1: 0, 2: 1, 3: 3})

	tests := []struct {
		name          string
		theirSubnets  commons.Subnets
		expectedScore float64
		description   string
	}{
		{
			name:          "peer with dead subnet",
			theirSubnets:  createSubnets(1),
			expectedScore: 90, // deadSubnetPriority
			description:   "Peer shares subnet 1 which is dead (0 peers)",
		},
		{
			name:          "peer with solo subnet",
			theirSubnets:  createSubnets(2),
			expectedScore: 3, // soloSubnetPriority
			description:   "Peer shares subnet 2 which is solo (1 peer)",
		},
		{
			name:          "peer with duo subnet",
			theirSubnets:  createSubnets(3),
			expectedScore: 0, // No score because we already have 3 peers
			description:   "Peer shares subnet 3 which has 3 peers (above duo threshold)",
		},
		{
			name:          "peer with dead and solo subnets",
			theirSubnets:  createSubnets(1, 2),
			expectedScore: 93, // deadSubnetPriority + soloSubnetPriority
			description:   "Peer shares subnet 1 (dead) and subnet 2 (solo)",
		},
		{
			name:          "peer with all subnets",
			theirSubnets:  createSubnets(1, 2, 3),
			expectedScore: 93, // deadSubnetPriority + soloSubnetPriority
			description:   "Peer shares all subnets, but only 1 (dead) and 2 (solo) contribute to score",
		},
		{
			name:          "peer with unsubscribed subnet",
			theirSubnets:  createSubnets(4),
			expectedScore: 0, // No shared subnets
			description:   "Peer doesn't share any of our subnets",
		},
		{
			name:          "peer with dead subnet and unsubscribed subnet",
			theirSubnets:  createSubnets(1, 4),
			expectedScore: 90, // deadSubnetPriority
			description:   "Peer shares subnet 1 (dead) and subnet 4 (not subscribed)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := ourSubnetPeers.Score(ourSubnets, tt.theirSubnets)
			require.Equal(t, tt.expectedScore, score, tt.description)
		})
	}
}

func TestSubnetPeers_Score_MixedSubnets(t *testing.T) {
	// Test case with a mix of subnet states

	// Our node is subscribed to subnets 0-9
	ourSubnets := createSubnets(0, 1, 2, 3, 4, 5, 6, 7, 8, 9)

	// We have different peer counts in each subnet:
	// - Dead (0 peers): 0, 1
	// - Solo (1 peer): 2, 3
	// - Duo (2 peers): 4, 5
	// - Healthy (3+ peers): 6, 7, 8, 9
	ourSubnetPeers := createSubnetPeers(map[int]uint16{
		0: 0, 1: 0, // Dead subnets
		2: 1, 3: 1, // Solo subnets
		4: 2, 5: 2, // Duo subnets
		6: 3, 7: 4, 8: 5, 9: 6, // Healthy subnets
	})

	tests := []struct {
		name          string
		theirSubnets  commons.Subnets
		expectedScore float64
		description   string
	}{
		{
			name:          "peer with all dead subnets",
			theirSubnets:  createSubnets(0, 1),
			expectedScore: 180, // 2 * deadSubnetPriority
			description:   "Peer shares both dead subnets",
		},
		{
			name:          "peer with all solo subnets",
			theirSubnets:  createSubnets(2, 3),
			expectedScore: 6, // 2 * soloSubnetPriority
			description:   "Peer shares both solo subnets",
		},
		{
			name:          "peer with all duo subnets",
			theirSubnets:  createSubnets(4, 5),
			expectedScore: 2, // 2 * duoSubnetPriority
			description:   "Peer shares both duo subnets",
		},
		{
			name:          "peer with all healthy subnets",
			theirSubnets:  createSubnets(6, 7, 8, 9),
			expectedScore: 0, // No score for healthy subnets
			description:   "Peer shares only healthy subnets",
		},
		{
			name:          "peer with mixed subnet types",
			theirSubnets:  createSubnets(0, 2, 4, 6),
			expectedScore: 94, // deadSubnetPriority + soloSubnetPriority + duoSubnetPriority
			description:   "Peer shares one of each subnet type",
		},
		{
			name:          "peer with highest priority subnets",
			theirSubnets:  createSubnets(0, 1, 2),
			expectedScore: 183, // 2 * deadSubnetPriority + soloSubnetPriority
			description:   "Peer shares both dead subnets and one solo subnet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := ourSubnetPeers.Score(ourSubnets, tt.theirSubnets)
			require.Equal(t, tt.expectedScore, score, tt.description)
		})
	}
}

func TestSubnetPeers_Score_PeerSelection(t *testing.T) {
	// Test case to simulate peer selection scenario

	// Our node is subscribed to subnets 0, 1, 2
	ourSubnets := createSubnets(0, 1, 2)

	// We have 0 peers in subnet 0, 1 peer in subnet 1, 2 peers in subnet 2
	ourSubnetPeers := createSubnetPeers(map[int]uint16{0: 0, 1: 1, 2: 2})

	// Define several potential peers with different subnet combinations
	peerA := createSubnets(0)       // Shares dead subnet
	peerB := createSubnets(1)       // Shares solo subnet
	peerC := createSubnets(2)       // Shares duo subnet
	peerD := createSubnets(0, 1)    // Shares dead and solo subnets
	peerE := createSubnets(0, 2)    // Shares dead and duo subnets
	peerF := createSubnets(1, 2)    // Shares solo and duo subnets
	peerG := createSubnets(0, 1, 2) // Shares all subnets
	peerH := createSubnets(3, 4, 5) // Shares no subnets

	// Calculate scores for each peer
	scoreA := ourSubnetPeers.Score(ourSubnets, peerA)
	scoreB := ourSubnetPeers.Score(ourSubnets, peerB)
	scoreC := ourSubnetPeers.Score(ourSubnets, peerC)
	scoreD := ourSubnetPeers.Score(ourSubnets, peerD)
	scoreE := ourSubnetPeers.Score(ourSubnets, peerE)
	scoreF := ourSubnetPeers.Score(ourSubnets, peerF)
	scoreG := ourSubnetPeers.Score(ourSubnets, peerG)
	scoreH := ourSubnetPeers.Score(ourSubnets, peerH)

	// Verify the peer selection priority
	// Expected order: D/G (dead+solo) > E (dead+duo) > A (dead) > F (solo+duo) > B (solo) > C (duo) > H (none)

	// Check that peers with dead subnets are prioritized
	require.True(t, scoreD >= scoreA, "Peer with dead+solo subnets should score higher than peer with only dead subnet")
	require.True(t, scoreE >= scoreA, "Peer with dead+duo subnets should score higher than peer with only dead subnet")
	require.True(t, scoreA > scoreB, "Peer with dead subnet should score higher than peer with solo subnet")
	require.True(t, scoreA > scoreC, "Peer with dead subnet should score higher than peer with duo subnet")

	// Check that peers with solo subnets are prioritized over duo subnets
	require.True(t, scoreB > scoreC, "Peer with solo subnet should score higher than peer with duo subnet")

	// Check that peers with more shared subnets score higher within same priority
	require.True(t, scoreD >= scoreB, "Peer with dead+solo subnets should score higher than peer with only solo subnet")
	require.True(t, scoreF >= scoreC, "Peer with solo+duo subnets should score higher than peer with only duo subnet")

	// Check that peer with all subnets scores highest
	require.True(t, scoreG >= scoreD, "Peer with all subnets should score at least as high as peer with dead+solo subnets")
	require.True(t, scoreG >= scoreE, "Peer with all subnets should score at least as high as peer with dead+duo subnets")
	require.True(t, scoreG >= scoreF, "Peer with all subnets should score at least as high as peer with solo+duo subnets")

	// Check that peer with no shared subnets scores lowest
	require.Equal(t, float64(0), scoreH, "Peer with no shared subnets should have zero score")
	require.True(t, scoreC > scoreH, "Even peer with only duo subnet should score higher than peer with no shared subnets")
}

func TestSubnetPeers_String(t *testing.T) {
	tests := []struct {
		name     string
		peers    SubnetPeers
		expected string
	}{
		{
			name:     "empty peers",
			peers:    SubnetPeers{},
			expected: "",
		},
		{
			name:     "single subnet",
			peers:    createSubnetPeers(map[int]uint16{5: 3}),
			expected: "5:3 ",
		},
		{
			name:     "multiple subnets",
			peers:    createSubnetPeers(map[int]uint16{1: 1, 5: 3, 10: 2}),
			expected: "1:1 5:3 10:2 ",
		},
		{
			name:     "zero value subnets are not included",
			peers:    createSubnetPeers(map[int]uint16{1: 0, 5: 3, 10: 0}),
			expected: "5:3 ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.peers.String()
			require.Equal(t, tt.expected, result)
		})
	}
}
