package connections

import (
	"context"
	"errors"
	"sync"
	"testing"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	ma "github.com/multiformats/go-multiaddr"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/network/peers/connections/mock"
	"github.com/ssvlabs/ssv/network/records"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
)

var errTestHandshake = errors.New("test handshake error")

type testConn struct {
	remotePeer      peer.ID
	localMultiaddr  ma.Multiaddr
	remoteMultiaddr ma.Multiaddr
	stats           libp2pnetwork.ConnStats
}

func (c *testConn) Close() error { return nil }

func (c *testConn) LocalPeer() peer.ID { return "" }

func (c *testConn) LocalPrivateKey() libp2pcrypto.PrivKey { return nil }

func (c *testConn) RemotePeer() peer.ID { return c.remotePeer }

func (c *testConn) RemotePublicKey() libp2pcrypto.PubKey { return nil }

func (c *testConn) ConnState() libp2pnetwork.ConnectionState { return libp2pnetwork.ConnectionState{} }

func (c *testConn) LocalMultiaddr() ma.Multiaddr { return c.localMultiaddr }

func (c *testConn) RemoteMultiaddr() ma.Multiaddr { return c.remoteMultiaddr }

func (c *testConn) Stat() libp2pnetwork.ConnStats { return c.stats }

func (c *testConn) Scope() libp2pnetwork.ConnScope { return nil }

func (c *testConn) ID() string { return "test-conn" }

func (c *testConn) NewStream(context.Context) (libp2pnetwork.Stream, error) { return nil, nil }

func (c *testConn) GetStreams() []libp2pnetwork.Stream { return nil }

func (c *testConn) IsClosed() bool { return false }

func (c *testConn) CloseWithError(libp2pnetwork.ConnErrorCode) error { return nil }

func (c *testConn) As(any) bool { return false }

type testHandshaker struct {
	mu      sync.Mutex
	calls   []peer.ID
	started chan struct{}
	release chan struct{}
	err     error
}

func (h *testHandshaker) Handshake(_ *zap.Logger, conn libp2pnetwork.Conn) error {
	h.mu.Lock()
	h.calls = append(h.calls, conn.RemotePeer())
	started := h.started
	release := h.release
	err := h.err
	h.mu.Unlock()

	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if release != nil {
		<-release
	}
	return err
}

func (h *testHandshaker) Handler() libp2pnetwork.StreamHandler { return nil }

func (h *testHandshaker) CallCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	return len(h.calls)
}

type testNetwork struct {
	mu            sync.Mutex
	connectedness map[peer.ID]libp2pnetwork.Connectedness
	closedPeers   []peer.ID
	peers         []peer.ID
}

func newTestNetwork() *testNetwork {
	return &testNetwork{
		connectedness: map[peer.ID]libp2pnetwork.Connectedness{},
	}
}

func (n *testNetwork) Peerstore() peerstore.Peerstore { return nil }

func (n *testNetwork) LocalPeer() peer.ID { return "local" }

func (n *testNetwork) DialPeer(context.Context, peer.ID) (libp2pnetwork.Conn, error) { return nil, nil }

func (n *testNetwork) ClosePeer(id peer.ID) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.closedPeers = append(n.closedPeers, id)
	n.connectedness[id] = libp2pnetwork.NotConnected
	return nil
}

func (n *testNetwork) Connectedness(id peer.ID) libp2pnetwork.Connectedness {
	n.mu.Lock()
	defer n.mu.Unlock()

	return n.connectedness[id]
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

func mustMultiaddr(addr string) ma.Multiaddr {
	return ma.StringCast(addr)
}

type TestData struct {
	NetworkPrivateKey libp2pcrypto.PrivKey
	SenderPrivateKey  keys.OperatorPrivateKey

	Signature [256]byte

	SenderPeerID    peer.ID
	RecipientPeerID peer.ID

	SenderBase64PublicKeyPEM string

	Handshaker handshaker
	Conn       mock.Conn

	NodeInfo *records.NodeInfo
}

func getTestingData(t *testing.T) TestData {
	peerID1 := peer.ID("1.1.1.1")
	peerID2 := peer.ID("2.2.2.2")

	privateKey, err := keys.GeneratePrivateKey()
	require.NoError(t, err)

	senderPublicKey, err := privateKey.Public().Base64()
	require.NoError(t, err)

	nodeInfo := &records.NodeInfo{
		NetworkID: "some-network-id",
		Metadata: &records.NodeMetadata{
			NodeVersion:   "some-node-version",
			ExecutionNode: "some-execution-node",
			ConsensusNode: "some-consensus-node",
			Subnets:       commons.AllSubnets.StringHex(),
		},
	}

	nii := mock.NodeInfoIndex{
		MockNodeInfo: &records.NodeInfo{
			NetworkID: "test-network-id",
			Metadata: &records.NodeMetadata{
				NodeVersion:   "test-node-version",
				ExecutionNode: "test-execution-node",
				ConsensusNode: "test-consensus-node",
				Subnets:       commons.AllSubnets.StringHex(),
			},
		},
		MockSelfSealed: []byte("something"),
	}
	ns := peers.NewPeerInfoIndex()
	ch := make(chan struct{})
	close(ch)
	ids := mock.IDService{
		MockIdentifyWait: ch,
	}
	ps := mock.Peerstore{
		ExistingPIDs:               []peer.ID{peerID2},
		MockFirstSupportedProtocol: "I support handshake protocol",
	}
	net := mock.Net{
		MockPeerstore: ps,
	}

	networkPrivateKey, _, err := libp2pcrypto.GenerateKeyPair(libp2pcrypto.ECDSA, 0)
	require.NoError(t, err)

	data, err := nodeInfo.Seal(networkPrivateKey)
	require.NoError(t, err)

	sc := mock.StreamController{
		MockRequest: data,
	}

	mockHandshaker := handshaker{
		ctx:                t.Context(),
		nodeInfos:          nii,
		peerInfos:          ns,
		subnetsIdx:         peers.NewSubnetsIndex(),
		ids:                ids,
		net:                net,
		streams:            sc,
		filters:            func() []HandshakeFilter { return []HandshakeFilter{} },
		domainTypeProvider: func() spectypes.DomainType { return networkconfig.TestNetwork.DomainType },
	}

	mockConn := mock.Conn{
		MockPeerID: peerID2,
	}

	return TestData{
		SenderPrivateKey:         privateKey,
		SenderPeerID:             peerID2,
		RecipientPeerID:          peerID1,
		SenderBase64PublicKeyPEM: senderPublicKey,
		Handshaker:               mockHandshaker,
		Conn:                     mockConn,
		NetworkPrivateKey:        networkPrivateKey,
		NodeInfo:                 nodeInfo,
	}
}
