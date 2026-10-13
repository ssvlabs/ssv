package p2pv1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/discovery"
)

// createSubnets creates a commons.Subnets with the specified subnets active.
func createSubnets(activeSubnets ...uint64) commons.Subnets {
	subnets := commons.Subnets{}
	for _, subnet := range activeSubnets {
		if subnet < commons.SubnetsCount {
			subnets.Set(subnet)
		}
	}
	return subnets
}

// createSubnetPeers creates a SubnetPeers with the specified number of peers on the
// Alan side of each subnet. Boole side stays zero. Use createBooleSubnetPeers to
// populate the Boole side.
func createSubnetPeers(peerCounts map[int]uint16) SubnetPeers {
	var peers SubnetPeers
	for subnet, count := range peerCounts {
		if subnet >= 0 && subnet < commons.SubnetsCount {
			peers.alan[subnet] = count
		}
	}
	return peers
}

// createBooleSubnetPeers is the Boole-side counterpart to createSubnetPeers.
func createBooleSubnetPeers(peerCounts map[int]uint16) SubnetPeers {
	var peers SubnetPeers
	for subnet, count := range peerCounts {
		if subnet >= 0 && subnet < commons.SubnetsCount {
			peers.boole[subnet] = count
		}
	}
	return peers
}

// scoreAlanOnly helps legacy tests that were written pre-Boole: it treats the given
// ours/theirs as Alan subnets only, which matches the semantic of the original
// single-bitfield SubnetPeers.Score(ours, theirs) method.
func scoreAlanOnly(a SubnetPeers, ours, theirs commons.Subnets) float64 {
	return a.Score(ours, commons.ZeroSubnets, theirs, commons.ZeroSubnets)
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
			a:        newSubnetPeers(),
			b:        newSubnetPeers(),
			expected: newSubnetPeers(),
		},
		{
			name:     "one subnet in a",
			a:        createSubnetPeers(map[int]uint16{5: 3}),
			b:        newSubnetPeers(),
			expected: createSubnetPeers(map[int]uint16{5: 3}),
		},
		{
			name:     "one subnet in b",
			a:        newSubnetPeers(),
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
			expectedScore: 16, // deadSubnetPriority
			description:   "Peer shares subnet 1 which is dead (0 peers)",
		},
		{
			name:          "peer with solo subnet",
			theirSubnets:  createSubnets(2),
			expectedScore: 4, // soloSubnetPriority
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
			expectedScore: 20, // deadSubnetPriority + soloSubnetPriority
			description:   "Peer shares subnet 1 (dead) and subnet 2 (solo)",
		},
		{
			name:          "peer with all subnets",
			theirSubnets:  createSubnets(1, 2, 3),
			expectedScore: 20, // deadSubnetPriority + soloSubnetPriority
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
			expectedScore: 16, // deadSubnetPriority
			description:   "Peer shares subnet 1 (dead) and subnet 4 (not subscribed)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := scoreAlanOnly(ourSubnetPeers, ourSubnets, tt.theirSubnets)
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
			expectedScore: 32, // 2 * deadSubnetPriority
			description:   "Peer shares both dead subnets",
		},
		{
			name:          "peer with all solo subnets",
			theirSubnets:  createSubnets(2, 3),
			expectedScore: 8, // 2 * soloSubnetPriority
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
			expectedScore: 21, // deadSubnetPriority + soloSubnetPriority + duoSubnetPriority
			description:   "Peer shares one of each subnet type",
		},
		{
			name:          "peer with highest priority subnets",
			theirSubnets:  createSubnets(0, 1, 2),
			expectedScore: 36, // 2 * deadSubnetPriority + soloSubnetPriority
			description:   "Peer shares both dead subnets and one solo subnet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := scoreAlanOnly(ourSubnetPeers, ourSubnets, tt.theirSubnets)
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
	scoreA := scoreAlanOnly(ourSubnetPeers, ourSubnets, peerA)
	scoreB := scoreAlanOnly(ourSubnetPeers, ourSubnets, peerB)
	scoreC := scoreAlanOnly(ourSubnetPeers, ourSubnets, peerC)
	scoreD := scoreAlanOnly(ourSubnetPeers, ourSubnets, peerD)
	scoreE := scoreAlanOnly(ourSubnetPeers, ourSubnets, peerE)
	scoreF := scoreAlanOnly(ourSubnetPeers, ourSubnets, peerF)
	scoreG := scoreAlanOnly(ourSubnetPeers, ourSubnets, peerG)
	scoreH := scoreAlanOnly(ourSubnetPeers, ourSubnets, peerH)

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
	// Format: "subnet:alan/boole", printed for every subnet index that has a non-zero
	// count on either side.
	tests := []struct {
		name     string
		peers    SubnetPeers
		expected string
	}{
		{
			name:     "empty peers",
			peers:    newSubnetPeers(),
			expected: "",
		},
		{
			name:     "single subnet alan side",
			peers:    createSubnetPeers(map[int]uint16{5: 3}),
			expected: "5:3/0",
		},
		{
			name:     "multiple subnets alan side",
			peers:    createSubnetPeers(map[int]uint16{1: 1, 5: 3, 10: 2}),
			expected: "1:1/0 5:3/0 10:2/0",
		},
		{
			name:     "zero value subnets are not included",
			peers:    createSubnetPeers(map[int]uint16{1: 0, 5: 3, 10: 0}),
			expected: "5:3/0",
		},
		{
			name:     "mixed alan and boole",
			peers:    createSubnetPeers(map[int]uint16{1: 2}).Add(createBooleSubnetPeers(map[int]uint16{1: 3, 5: 1})),
			expected: "1:2/3 5:0/1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.peers.String()
			require.Equal(t, tt.expected, result)
		})
	}
}

// TestSubnetPeers_Score_BooleTransition covers the fork-transition case where
// Alan-N and Boole-N share the subnet index but are independent gossipsub topics.
// Before this refactor the two sides were summed, which hid a dead side behind a
// healthy side and mis-scored peers.
func TestSubnetPeers_Score_BooleTransition(t *testing.T) {
	// We're in the prior window: subscribed to Alan-5, Alan-7, Boole-5, Boole-42.
	ourAlan := createSubnets(5, 7)
	ourBoole := createSubnets(5, 42)

	// Alan-5 is dead (0 peers) while Boole-5 has 10 peers.
	// Alan-7 has 1 peer; Boole-42 is dead.
	peers := createSubnetPeers(map[int]uint16{5: 0, 7: 1}).
		Add(createBooleSubnetPeers(map[int]uint16{5: 10, 42: 0}))

	t.Run("peer advertising bit 5 is credited for dead Alan-5 even though Boole-5 is healthy", func(t *testing.T) {
		// Peer ENR: bit 5. Could be Alan-5, Boole-5, or both.
		peerENR := createSubnets(5)
		// Credit: Alan-5 dead (+16) + Boole-5 healthy (0). Pre-refactor: sum=10 -> no credit.
		got := peers.Score(ourAlan, ourBoole, peerENR, peerENR)
		require.Equal(t, 16.0, got, "dead Alan-5 must not be hidden by healthy Boole-5")
	})

	t.Run("peer advertising bit 42 is credited for dead Boole-42", func(t *testing.T) {
		peerENR := createSubnets(42)
		// Credit: Alan-42 not subscribed -> skip; Boole-42 dead (+16).
		got := peers.Score(ourAlan, ourBoole, peerENR, peerENR)
		require.Equal(t, 16.0, got)
	})

	t.Run("peer advertising bit 7 is credited for solo Alan-7", func(t *testing.T) {
		peerENR := createSubnets(7)
		// Credit: Alan-7 solo (+4); Boole-7 not subscribed -> skip.
		got := peers.Score(ourAlan, ourBoole, peerENR, peerENR)
		require.Equal(t, 4.0, got)
	})

	t.Run("peer covering multiple bits sums per-fork credits", func(t *testing.T) {
		peerENR := createSubnets(5, 7, 42)
		// 16 (Alan-5 dead) + 0 (Boole-5 healthy) + 4 (Alan-7 solo) + 16 (Boole-42 dead) = 36.
		got := peers.Score(ourAlan, ourBoole, peerENR, peerENR)
		require.Equal(t, 36.0, got)
	})

	t.Run("observed trim-side scoring credits only actually-served forks", func(t *testing.T) {
		// Peer is observed on Alan-5 only (not Boole-5). Trim-scoring uses the precise
		// observed participation rather than the ENR union -- so we credit only Alan-5.
		observedAlan := createSubnets(5)
		observedBoole := commons.ZeroSubnets
		got := peers.Score(ourAlan, ourBoole, observedAlan, observedBoole)
		require.Equal(t, 16.0, got, "peer only on Alan-5 should not be credited for Boole-5")
	})
}

func TestPeerSelectionScore(t *testing.T) {
	ownAlanSubnets := createSubnets(1)
	ownBooleSubnets := commons.ZeroSubnets
	currentSubnetPeers := createSubnetPeers(map[int]uint16{1: 0})
	peerSubnets := createSubnets(1)
	now := time.Now()

	t.Run("blocks peers still in cooldown", func(t *testing.T) {
		score, ready := peerSelectionScore(now, discovery.DiscoveredPeer{
			Tries:   1,
			LastTry: now.Add(-peerSelectionRetryCooldownMin / 2),
		}, currentSubnetPeers, ownAlanSubnets, ownBooleSubnets, peerSubnets)
		require.False(t, ready)
		require.Zero(t, score)
	})

	t.Run("applies retry penalty after cooldown", func(t *testing.T) {
		score, ready := peerSelectionScore(now, discovery.DiscoveredPeer{
			Tries:   2,
			LastTry: now.Add(-45 * time.Second),
		}, currentSubnetPeers, ownAlanSubnets, ownBooleSubnets, peerSubnets)
		require.True(t, ready)
		require.Equal(t, 9.0, score)
	})
}
