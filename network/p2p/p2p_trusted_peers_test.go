package p2pv1

import (
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/peers"
	p2ptesting "github.com/ssvlabs/ssv/network/testing"
	"github.com/ssvlabs/ssv/network/topics/params"
)

// badGossipScore is a gossip score that makes a peer bad.
var badGossipScore = params.PeerScoreThresholds().GraylistThreshold

// TestParseTrustedPeers checks that the addresses of a peer listed in several TrustedPeers entries end up together
// under its ID, and that an entry that isn't a peer's multiaddr is an error.
func TestParseTrustedPeers(t *testing.T) {
	const (
		idA = "16Uiu2HAkvaBh2xjstjs1koEx3jWUkVM9AMqSTE8Jn2ZTDyy5Zo3d"
		idB = "16Uiu2HAm4D4KPNAXbz7Ez9DCRZeM3jyxuzcYQjxcqgPMmA6FyeX3"
	)
	a1, a2 := "/ip4/10.0.0.1/tcp/13001/p2p/"+idA, "/dns4/a.example.com/tcp/13001/p2p/"+idA
	b1 := "/ip4/10.0.0.2/tcp/13001/p2p/" + idB

	n := &p2pNetwork{cfg: &Config{TrustedPeers: []string{a1 + "," + b1, a2}}}
	require.NoError(t, n.parseTrustedPeers())

	addrs := func(id string) []ma.Multiaddr {
		pid, err := peer.Decode(id)
		require.NoError(t, err)
		return n.trustedPeers[pid].Addrs
	}
	require.Len(t, n.trustedPeers, 2)
	require.Equal(t, []ma.Multiaddr{ma.StringCast(strings.TrimSuffix(a1, "/p2p/"+idA)), ma.StringCast(strings.TrimSuffix(a2, "/p2p/"+idA))}, addrs(idA))
	require.Equal(t, []ma.Multiaddr{ma.StringCast(strings.TrimSuffix(b1, "/p2p/"+idB))}, addrs(idB))

	n = &p2pNetwork{cfg: &Config{TrustedPeers: []string{"/ip4/10.0.0.1/tcp/13001"}}}
	require.ErrorContains(t, n.parseTrustedPeers(), "could not parse trusted peer")
}

// TestTrustedPeers_Redialed checks that a node dials its trusted peer, and dials it again once it disconnects, even
// with a score that keeps any other peer from being dialed or handshaken with.
func TestTrustedPeers_Redialed(t *testing.T) {
	keys, err := p2ptesting.CreateKeys(2)
	require.NoError(t, err)
	trusted := newTrustedPeersTestNode(t, keys, 0)
	require.NoError(t, trusted.Start())
	// Start would run keepTrustedPeersConnected too, so the node stays set up but unstarted: its connections are
	// handled all the same, and the loop under test is the only one dialing.
	node := newTrustedPeersTestNode(t, keys, 1, trusted)
	trustedID := trusted.Host().ID()

	node.keepTrustedPeersConnected(0, 100*time.Millisecond)
	require.Eventually(t, func() bool {
		return node.idx.State(trustedID) == peers.StateConnected
	}, 10*time.Second, 50*time.Millisecond, "the trusted peer was never dialed")
	firstConn := node.Host().Network().ConnsToPeer(trustedID)[0].ID()

	node.idx.SetScores(map[peer.ID]float64{trustedID: badGossipScore})
	require.NoError(t, node.Host().Network().ClosePeer(trustedID))

	require.Eventually(t, func() bool {
		conns := node.Host().Network().ConnsToPeer(trustedID)
		return len(conns) == 1 && conns[0].ID() != firstConn && node.idx.State(trustedID) == peers.StateConnected
	}, 10*time.Second, 50*time.Millisecond, "the trusted peer wasn't dialed again")
}

// newTrustedPeersTestNode creates and sets up node i of a local net, trusting the given nodes. Its cleanup closes it.
func newTrustedPeersTestNode(t *testing.T, keys []p2ptesting.NodeKeys, i uint64, trusted ...*p2pNetwork) *p2pNetwork {
	t.Helper()

	trustedPeers := make([]string, 0, len(trusted))
	for _, tn := range trusted {
		addrs, err := peer.AddrInfoToP2pAddrs(&peer.AddrInfo{ID: tn.Host().ID(), Addrs: tn.Host().Addrs()})
		require.NoError(t, err)
		entry := make([]string, len(addrs))
		for j, addr := range addrs {
			entry[j] = addr.String()
		}
		trustedPeers = append(trustedPeers, strings.Join(entry, ","))
	}

	node, err := (&LocalNet{}).NewTestP2pNetwork(t.Context(), i, keys[i], zap.NewNop(), LocalNetOptions{
		Nodes:        len(keys),
		TrustedPeers: trustedPeers,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, node.Close()) })
	return node.(*p2pNetwork)
}
