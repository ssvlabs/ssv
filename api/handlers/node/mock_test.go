package node

import (
	"context"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/protocol"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/records"
)

// MockP2PNetwork is a simple value-based mock implementation of p2pNetwork.
// Each method returns the corresponding exported field value.
type MockP2PNetwork struct {
	// Returned by LocalPeer()
	LocalPeerValue peer.ID

	// Returned by ListenAddresses()
	ListenAddressesValue []ma.Multiaddr

	// Returned by Peers()
	PeersValue []peer.ID

	// Returned by Connectedness(peer.ID)
	// If the peer is not present, 0 is returned.
	ConnectednessByPeer map[peer.ID]network.Connectedness

	// Returned by Peerstore()
	PeerstoreValue peerstore.Peerstore

	// Returned by ConnsToPeer(peer.ID)
	// If the peer is not present, nil is returned.
	ConnsToPeerByPeer map[peer.ID][]network.Conn
}

func (m *MockP2PNetwork) LocalPeer() peer.ID {
	return m.LocalPeerValue
}

func (m *MockP2PNetwork) ListenAddresses() []ma.Multiaddr {
	return m.ListenAddressesValue
}

func (m *MockP2PNetwork) Peers() []peer.ID {
	return m.PeersValue
}

func (m *MockP2PNetwork) Connectedness(id peer.ID) network.Connectedness {
	if m.ConnectednessByPeer != nil {
		if c, ok := m.ConnectednessByPeer[id]; ok {
			return c
		}
	}
	return 0
}

func (m *MockP2PNetwork) Peerstore() peerstore.Peerstore {
	return m.PeerstoreValue
}

func (m *MockP2PNetwork) ConnsToPeer(p peer.ID) []network.Conn {
	if m.ConnsToPeerByPeer != nil {
		if conns, ok := m.ConnsToPeerByPeer[p]; ok {
			return conns
		}
	}
	return nil
}

type MockPeerstore struct {
	AddrsByPeer map[peer.ID][]ma.Multiaddr
}

func (m *MockPeerstore) Addrs(p peer.ID) []ma.Multiaddr {
	if m.AddrsByPeer == nil {
		return nil
	}
	return m.AddrsByPeer[p]
}

func (*MockPeerstore) AddAddr(peer.ID, ma.Multiaddr, time.Duration)            {}
func (*MockPeerstore) AddAddrs(peer.ID, []ma.Multiaddr, time.Duration)         {}
func (*MockPeerstore) SetAddr(peer.ID, ma.Multiaddr, time.Duration)            {}
func (*MockPeerstore) SetAddrs(peer.ID, []ma.Multiaddr, time.Duration)         {}
func (*MockPeerstore) UpdateAddrs(peer.ID, time.Duration, time.Duration)       {}
func (*MockPeerstore) ClearAddrs(peer.ID)                                      {}
func (*MockPeerstore) AddrStream(context.Context, peer.ID) <-chan ma.Multiaddr { return nil }

func (*MockPeerstore) AddPrivKey(peer.ID, crypto.PrivKey) error { return nil }
func (*MockPeerstore) AddPubKey(peer.ID, crypto.PubKey) error   { return nil }
func (*MockPeerstore) PrivKey(peer.ID) crypto.PrivKey           { return nil }
func (*MockPeerstore) PubKey(peer.ID) crypto.PubKey             { return nil }

func (*MockPeerstore) AddProtocols(peer.ID, ...protocol.ID) error    { return nil }
func (*MockPeerstore) RemoveProtocols(peer.ID, ...protocol.ID) error { return nil }
func (*MockPeerstore) SetProtocols(peer.ID, ...protocol.ID) error    { return nil }
func (*MockPeerstore) GetProtocols(peer.ID) ([]protocol.ID, error)   { return nil, nil }
func (*MockPeerstore) SupportsProtocols(peer.ID, ...protocol.ID) ([]protocol.ID, error) {
	return nil, nil
}
func (*MockPeerstore) FirstSupportedProtocol(peer.ID, ...protocol.ID) (protocol.ID, error) {
	return "", nil
}

func (*MockPeerstore) Put(peer.ID, string, any) error   { return nil }
func (*MockPeerstore) Get(peer.ID, string) (any, error) { return nil, nil }

func (*MockPeerstore) RecordLatency(peer.ID, time.Duration) {}
func (*MockPeerstore) LatencyEWMA(peer.ID) time.Duration    { return 0 }

func (*MockPeerstore) PeerInfo(peer.ID) peer.AddrInfo { return peer.AddrInfo{} }
func (*MockPeerstore) Peers() peer.IDSlice            { return nil }
func (*MockPeerstore) PeersWithAddrs() peer.IDSlice   { return nil }
func (*MockPeerstore) PeersWithKeys() peer.IDSlice    { return nil }
func (*MockPeerstore) RemovePeer(peer.ID)             {}

func (*MockPeerstore) Close() error { return nil }

// MockTopicIndex is a simple mock implementation of TopicIndex.
type MockTopicIndex struct {
	peersByTopic map[string][]peer.ID
}

// PeersByTopic satisfies the TopicIndex interface.
func (m *MockTopicIndex) PeersByTopic() map[string][]peer.ID {
	return m.peersByTopic
}

// MockPeersIndex is a simple mock implementation of peersIndex.
type MockPeersIndex struct {
	self        *records.NodeInfo
	nodeInfo    *records.NodeInfo
	peerSubnets commons.Subnets
}

func (m *MockPeersIndex) Self() *records.NodeInfo {
	return m.self
}

func (m *MockPeersIndex) NodeInfo(id peer.ID) *records.NodeInfo {
	return m.nodeInfo
}

func (m *MockPeersIndex) GetPeerSubnets(id peer.ID) (commons.Subnets, bool) {
	return m.peerSubnets, true
}
