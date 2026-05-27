package connections

import (
	"testing"
	"time"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/network/peers/connections/mock"
	"github.com/ssvlabs/ssv/network/records"
	"github.com/ssvlabs/ssv/observability/log"
)

// recordingNodeInfoIndex wraps mock.NodeInfoIndex and records every
// SetNodeInfo call so tests can assert directly on storage rather than
// inferring it from a downstream side-effect.
type recordingNodeInfoIndex struct {
	mock.NodeInfoIndex
	setCalls []peer.ID
}

func (r *recordingNodeInfoIndex) SetNodeInfo(id peer.ID, _ *records.NodeInfo) {
	r.setCalls = append(r.setCalls, id)
}

var _ peers.NodeInfoIndex = (*recordingNodeInfoIndex)(nil)

// TestHandshakeTestData is a test for testing data and mocks
func TestHandshakeTestData(t *testing.T) {
	testLogger := log.TestLogger(t)

	t.Run("happy flow", func(t *testing.T) {
		td := getTestingData(t)

		beforeHandshake := time.Now()
		require.NoError(t, td.Handshaker.Handshake(testLogger, td.Conn))

		pi := td.Handshaker.peerInfos.PeerInfo(td.SenderPeerID)
		require.NotNil(t, pi)
		require.True(t, pi.LastHandshake.After(beforeHandshake) && pi.LastHandshake.Before(time.Now()))
		require.Nil(t, pi.LastHandshakeError)
	})

	t.Run("wrong NodeInfoIndex", func(t *testing.T) {
		td := getTestingData(t)

		beforeHandshake := time.Now()
		td.Handshaker.nodeInfos = mock.NodeInfoIndex{}
		require.Error(t, td.Handshaker.Handshake(testLogger, td.Conn))

		pi := td.Handshaker.peerInfos.PeerInfo(td.SenderPeerID)
		require.NotNil(t, pi)
		require.True(t, pi.LastHandshake.After(beforeHandshake) && pi.LastHandshake.Before(time.Now()))
		require.Error(t, pi.LastHandshakeError)
	})

	t.Run("wrong StreamController", func(t *testing.T) {
		td := getTestingData(t)
		td.Handshaker.streams = mock.StreamController{}
		require.Error(t, td.Handshaker.Handshake(testLogger, td.Conn))
	})
}

// TestVerifyTheirNodeInfo_RejectsNilMetadata covers the case where a peer sends
// a NodeInfo envelope without a Metadata block (only ForkVersion + NetworkID).
// We reject such peers at handshake time so the partially-populated NodeInfo
// never lands in the peers index, where readers like the /v1/node/peers API
// would otherwise nil-deref on `.Metadata.NodeVersion`.
func TestVerifyTheirNodeInfo_RejectsNilMetadata(t *testing.T) {
	testLogger := log.TestLogger(t)
	td := getTestingData(t)

	// Swap the default mock NodeInfoIndex for a recording variant so we can
	// assert directly that SetNodeInfo was never called on the rejection path.
	base, ok := td.Handshaker.nodeInfos.(mock.NodeInfoIndex)
	require.True(t, ok, "expected getTestingData to wire mock.NodeInfoIndex")
	recorder := &recordingNodeInfoIndex{NodeInfoIndex: base}
	td.Handshaker.nodeInfos = recorder

	// Build a NodeInfo with Metadata == nil and have the (mocked) stream
	// controller return its sealed bytes — that's what Consume on the local
	// side will parse and pass to verifyTheirNodeInfo.
	nodeInfoNoMeta := &records.NodeInfo{
		NetworkID: "some-network-id",
		// Metadata intentionally left nil
	}
	networkPrivateKey, _, err := libp2pcrypto.GenerateKeyPair(libp2pcrypto.ECDSA, 0)
	require.NoError(t, err)
	sealed, err := nodeInfoNoMeta.Seal(networkPrivateKey)
	require.NoError(t, err)
	td.Handshaker.streams = mock.StreamController{MockRequest: sealed}

	beforeHandshake := time.Now()
	err = td.Handshaker.Handshake(testLogger, td.Conn)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing metadata")

	// The handshake should be recorded as failed on the peer-info side so the
	// connection handler will reject the connection.
	pi := td.Handshaker.peerInfos.PeerInfo(td.SenderPeerID)
	require.NotNil(t, pi)
	require.True(t, pi.LastHandshake.After(beforeHandshake))
	require.Error(t, pi.LastHandshakeError)

	// Crucially: verify that we did NOT store a NodeInfo for this peer.
	// Direct assertion via the recording index — no proxy needed.
	require.Empty(t, recorder.setCalls, "SetNodeInfo must not be called when handshake is rejected")
}

// TestPeerAgentVersion exercises the AgentVersion lookup helper across the
// realistic libp2p peerstore states: identify hasn't populated yet (error),
// identify has populated with a string, and the network/peerstore are nil
// (defensive paths). The helper is expected to degrade to "" in every failure
// mode, since it's used purely for log diagnostics.
func TestPeerAgentVersion(t *testing.T) {
	t.Run("nil network", func(t *testing.T) {
		require.Equal(t, "", peerAgentVersion(nil, "any"))
	})

	t.Run("peerstore returns non-string", func(t *testing.T) {
		// The default mock peerstore configured in getTestingData includes
		// SenderPeerID in ExistingPIDs and returns the peer.ID itself for any
		// key — not a string, so the helper's type assertion fails and it
		// yields "".
		td := getTestingData(t)
		require.Equal(t, "", peerAgentVersion(td.Handshaker.net, td.SenderPeerID))
	})

	t.Run("peerstore returns configured string", func(t *testing.T) {
		td := getTestingData(t)
		ps := mock.Peerstore{
			ExistingPIDs: []peer.ID{td.SenderPeerID},
			MockValues: map[peer.ID]map[string]any{
				td.SenderPeerID: {"AgentVersion": "ssv/v2.4.2"},
			},
		}
		td.Handshaker.net = mock.Net{MockPeerstore: ps}
		require.Equal(t, "ssv/v2.4.2", peerAgentVersion(td.Handshaker.net, td.SenderPeerID))
	})

	t.Run("peerstore returns error", func(t *testing.T) {
		td := getTestingData(t)
		ps := mock.Peerstore{
			// No ExistingPIDs → Get returns an error for any (pid, key).
		}
		td.Handshaker.net = mock.Net{MockPeerstore: ps}
		require.Equal(t, "", peerAgentVersion(td.Handshaker.net, td.SenderPeerID))
	})
}
