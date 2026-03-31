package peers

import (
	"context"
	"errors"
	"sync"
	"testing"

	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/commons"
)

var errTestClosePeer = errors.New("test close peer error")

type testNetwork struct {
	mu            sync.Mutex
	peers         []peer.ID
	closedPeers   []peer.ID
	closePeerErrs map[peer.ID]error
}

func (n *testNetwork) Peerstore() peerstore.Peerstore { return nil }

func (n *testNetwork) LocalPeer() peer.ID { return "local" }

func (n *testNetwork) DialPeer(context.Context, peer.ID) (libp2pnetwork.Conn, error) { return nil, nil }

func (n *testNetwork) ClosePeer(id peer.ID) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if err := n.closePeerErrs[id]; err != nil {
		return err
	}
	n.closedPeers = append(n.closedPeers, id)
	return nil
}

func (n *testNetwork) Connectedness(peer.ID) libp2pnetwork.Connectedness {
	return libp2pnetwork.NotConnected
}

func (n *testNetwork) Peers() []peer.ID {
	n.mu.Lock()
	defer n.mu.Unlock()

	peers := make([]peer.ID, len(n.peers))
	copy(peers, n.peers)
	return peers
}

func (n *testNetwork) Conns() []libp2pnetwork.Conn { return nil }

func (n *testNetwork) ConnsToPeer(peer.ID) []libp2pnetwork.Conn { return nil }

func (n *testNetwork) Notify(libp2pnetwork.Notifiee) {}

func (n *testNetwork) StopNotify(libp2pnetwork.Notifiee) {}

func (n *testNetwork) CanDial(peer.ID, ma.Multiaddr) bool { return true }

func (n *testNetwork) Close() error { return nil }

func (n *testNetwork) SetStreamHandler(libp2pnetwork.StreamHandler) {}

func (n *testNetwork) NewStream(context.Context, peer.ID) (libp2pnetwork.Stream, error) {
	return nil, nil
}

func (n *testNetwork) Listen(...ma.Multiaddr) error { return nil }

func (n *testNetwork) ListenAddresses() []ma.Multiaddr { return nil }

func (n *testNetwork) InterfaceListenAddresses() ([]ma.Multiaddr, error) { return nil, nil }

func (n *testNetwork) ResourceManager() libp2pnetwork.ResourceManager { return nil }

func (n *testNetwork) ClosedPeers() []peer.ID {
	n.mu.Lock()
	defer n.mu.Unlock()

	closedPeers := make([]peer.ID, len(n.closedPeers))
	copy(closedPeers, n.closedPeers)
	return closedPeers
}

type testGossipScoreIndex struct {
	badPeers map[peer.ID]float64
}

func (i *testGossipScoreIndex) SetScores(map[peer.ID]float64) {}

func (i *testGossipScoreIndex) GetGossipScore(peerID peer.ID) (float64, bool) {
	score, ok := i.badPeers[peerID]
	return score, ok
}

func (i *testGossipScoreIndex) HasBadGossipScore(peerID peer.ID) (bool, float64) {
	score, ok := i.badPeers[peerID]
	return ok, score
}

func TestConnManagerTrimPeers(t *testing.T) {
	p1 := peer.ID("peer-1")
	p2 := peer.ID("peer-2")
	p3 := peer.ID("peer-3")
	net := &testNetwork{peers: []peer.ID{p1, p2, p3}}

	manager := connManager{
		logger: zap.NewNop(),
	}

	manager.TrimPeers(t.Context(), net, map[peer.ID]struct{}{
		p1: {},
		p3: {},
	})

	require.ElementsMatch(t, []peer.ID{p1, p3}, net.ClosedPeers())
}

func TestConnManagerDisconnectFromBadPeers(t *testing.T) {
	p1 := peer.ID("peer-1")
	p2 := peer.ID("peer-2")
	p3 := peer.ID("peer-3")
	net := &testNetwork{}

	manager := connManager{
		logger:           zap.NewNop(),
		gossipScoreIndex: &testGossipScoreIndex{badPeers: map[peer.ID]float64{p1: -101, p3: -202}},
	}

	disconnected := manager.DisconnectFromBadPeers(net, []peer.ID{p1, p2, p3})

	require.Equal(t, 2, disconnected)
	require.ElementsMatch(t, []peer.ID{p1, p3}, net.ClosedPeers())
}

func TestConnManagerDisconnectFromIrrelevantPeersRespectsQuota(t *testing.T) {
	p1 := peer.ID("peer-1")
	p2 := peer.ID("peer-2")
	p3 := peer.ID("peer-3")

	subnetsIdx := NewSubnetsIndex()
	sharedSubnets := commons.ZeroSubnets
	sharedSubnets.Set(1)
	irrelevantSubnetsA := commons.ZeroSubnets
	irrelevantSubnetsA.Set(2)
	irrelevantSubnetsB := commons.ZeroSubnets
	irrelevantSubnetsB.Set(3)

	subnetsIdx.UpdatePeerSubnets(p1, sharedSubnets)
	subnetsIdx.UpdatePeerSubnets(p2, irrelevantSubnetsA)
	subnetsIdx.UpdatePeerSubnets(p3, irrelevantSubnetsB)

	manager := connManager{
		logger:     zap.NewNop(),
		subnetsIdx: subnetsIdx,
	}
	net := &testNetwork{}

	disconnected := manager.DisconnectFromIrrelevantPeers(1, net, []peer.ID{p1, p2, p3}, sharedSubnets)

	require.Equal(t, 1, disconnected)
	require.Equal(t, []peer.ID{p2}, net.ClosedPeers())
}

func TestConnManagerDisconnectFromIrrelevantPeersDisconnectsPeersWithUnknownSubnets(t *testing.T) {
	unknownPeer := peer.ID("peer-unknown")
	irrelevantPeer := peer.ID("peer-irrelevant")

	subnetsIdx := NewSubnetsIndex()
	mySubnets := commons.ZeroSubnets
	mySubnets.Set(1)
	irrelevantSubnets := commons.ZeroSubnets
	irrelevantSubnets.Set(2)
	subnetsIdx.UpdatePeerSubnets(irrelevantPeer, irrelevantSubnets)

	manager := connManager{
		logger:     zap.NewNop(),
		subnetsIdx: subnetsIdx,
	}
	net := &testNetwork{}

	disconnected := manager.DisconnectFromIrrelevantPeers(2, net, []peer.ID{unknownPeer, irrelevantPeer}, mySubnets)

	require.Equal(t, 2, disconnected)
	require.Equal(t, []peer.ID{unknownPeer, irrelevantPeer}, net.ClosedPeers())
}

func TestConnManagerDisconnectFromIrrelevantPeersDoesNotCountCloseErrorsAgainstQuota(t *testing.T) {
	p1 := peer.ID("peer-1")
	p2 := peer.ID("peer-2")
	p3 := peer.ID("peer-3")

	mySubnets := commons.ZeroSubnets
	mySubnets.Set(1)
	irrelevantSubnets := commons.ZeroSubnets
	irrelevantSubnets.Set(2)

	subnetsIdx := NewSubnetsIndex()
	subnetsIdx.UpdatePeerSubnets(p1, irrelevantSubnets)
	subnetsIdx.UpdatePeerSubnets(p2, irrelevantSubnets)
	subnetsIdx.UpdatePeerSubnets(p3, irrelevantSubnets)

	manager := connManager{
		logger:     zap.NewNop(),
		subnetsIdx: subnetsIdx,
	}
	net := &testNetwork{
		closePeerErrs: map[peer.ID]error{
			p1: errTestClosePeer,
		},
	}

	disconnected := manager.DisconnectFromIrrelevantPeers(2, net, []peer.ID{p1, p2, p3}, mySubnets)

	require.Equal(t, 2, disconnected)
	require.Equal(t, []peer.ID{p2, p3}, net.ClosedPeers())
}
