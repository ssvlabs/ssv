package node

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/records"
	"github.com/ssvlabs/ssv/nodeprobe"
)

// CreateTestNode builds a test Node using a local network.
func CreateTestNode(t *testing.T) *Node {
	nodeMock := &NodeMock{}
	nodeMock.HealthyMock.Store(nil)
	nodeProber := nodeprobe.New(zap.L())
	const node1 = "node_1"
	const node2 = "node_2"
	const node3 = "node_3"
	nodeProber.AddNode(node1, nodeMock, 10*time.Second, 5, 0)
	nodeProber.AddNode(node2, nodeMock, 10*time.Second, 5, 0)
	nodeProber.AddNode(node3, nodeMock, 10*time.Second, 5, 0)

	pIndex := &MockPeersIndex{
		self: &records.NodeInfo{
			NetworkID: "self",
			Metadata: &records.NodeMetadata{
				NodeVersion:   "self",
				ExecutionNode: "self",
				ConsensusNode: "self",
				Subnets:       "self",
			},
		},
		nodeInfo: &records.NodeInfo{
			NetworkID: "mainnet",
			Metadata: &records.NodeMetadata{
				NodeVersion:   "latest",
				ExecutionNode: "latest",
				ConsensusNode: "latest",
				Subnets:       "00000000000000000100000400000400",
			},
		},
		peerSubnets: commons.AllSubnets,
	}

	ownPeerID, err := peer.Decode("16Uiu2HAmH9JrTKfYWKB9ewbbE5xRCRrLRkwrNywvMqMk8vo5vqU2")
	require.NoError(t, err)
	peer1ID, err := peer.Decode("12D3KooWHMqRy1xSTtoeey9HMYNWkLGToMmTJFccX2zxGQPz2S57")
	require.NoError(t, err)
	peer2ID, err := peer.Decode("12D3KooWPxxZ6TgcCjCp8JeEEATAFLtriNLGumBroBYYMXLyNrxH")
	require.NoError(t, err)

	net := &MockP2PNetwork{
		LocalPeerValue: ownPeerID,
		ListenAddressesValue: []ma.Multiaddr{
			ma.StringCast("/ip4/1.2.3.4"),
		},
		PeersValue: []peer.ID{peer1ID, peer2ID},
		ConnectednessByPeer: map[peer.ID]libp2pnetwork.Connectedness{
			peer1ID: libp2pnetwork.Connected,
			peer2ID: libp2pnetwork.Connected,
		},
		PeerstoreValue: &MockPeerstore{
			AddrsByPeer: map[peer.ID][]ma.Multiaddr{
				peer1ID: {ma.StringCast("/ip4/1.2.3.5")},
				peer2ID: {ma.StringCast("/ip4/1.2.3.6")},
			},
		},
		ConnsToPeerByPeer: nil,
	}

	tIndex := &MockTopicIndex{
		peersByTopic: map[string][]peer.ID{
			"topic 1": {peer1ID, peer2ID},
		},
	}

	return NewNode(
		[]string{
			fmt.Sprintf("tcp://%s:%d", "localhost", 3030),
			fmt.Sprintf("udp://%s:%d", "localhost", 3030),
		},
		pIndex,
		net,
		tIndex,
		nodeProber,
		node1,
		node2,
		node3,
	)
}

// NodeMock is a dummy implementation of nodeprobe.Node.
type NodeMock struct {
	HealthyMock atomic.Pointer[error]
}

func (nm *NodeMock) Healthy(context.Context) error {
	if err := nm.HealthyMock.Load(); err != nil {
		return *err
	}

	return nil
}

// Type aliases for JSON response types.
type nodeIdentity = identityJSON
type peerInfo = peerJSON
type allPeersAndTopics = AllPeersAndTopicsJSON

// TestNodeHandlers verifies the endpoints of the Node (identity, peers, health, topics).
func TestNodeHandlers(t *testing.T) {
	node := CreateTestNode(t)

	tests := []struct {
		name    string
		method  string
		url     string
		handler http.HandlerFunc
		verify  func(t *testing.T, body []byte)
	}{
		{
			name:    "identity",
			method:  "GET",
			url:     "/v1/node/identity",
			handler: api.Handler(node.Identity),
			verify: func(t *testing.T, body []byte) {
				var resp nodeIdentity

				require.NoError(t, json.Unmarshal(body, &resp))
				require.NotEmpty(t, resp.PeerID)
			},
		},
		{
			name:    "peers",
			method:  "GET",
			url:     "/v1/node/peers",
			handler: api.Handler(node.Peers),
			verify: func(t *testing.T, body []byte) {
				var peers []peerInfo

				require.NoError(t, json.Unmarshal(body, &peers))
				require.GreaterOrEqual(t, len(peers), 1)
			},
		},
		{
			name:    "health",
			method:  "GET",
			url:     "/v1/node/health",
			handler: api.Handler(node.Health),
			verify: func(t *testing.T, body []byte) {
				var health struct {
					P2P           string `json:"p2p"`
					BeaconNode    string `json:"beacon_node"`
					ExecutionNode string `json:"execution_node"`
					EventSyncer   string `json:"event_syncer"`
					Advanced      struct {
						Peers           int      `json:"peers"`
						InboundConns    int      `json:"inbound_conns"`
						OutboundConns   int      `json:"outbound_conns"`
						ListenAddresses []string `json:"p2p_listen_addresses"`
					} `json:"advanced"`
				}

				require.NoError(t, json.Unmarshal(body, &health))
			},
		},
		{
			name:    "topics",
			method:  "GET",
			url:     "/v1/node/topics",
			handler: api.Handler(node.Topics),
			verify: func(t *testing.T, body []byte) {
				var topics allPeersAndTopics

				require.NoError(t, json.Unmarshal(body, &topics))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, tt.url, nil)

			require.NoError(t, err)

			rr := httptest.NewRecorder()
			tt.handler.ServeHTTP(rr, req)

			require.Equal(t, http.StatusOK, rr.Code)

			tt.verify(t, rr.Body.Bytes())
		})
	}
}

// TestHealthCheckJSONString verifies that healthCheckJSON.String() returns correctly formatted JSON.
func TestHealthCheckJSONString(t *testing.T) {
	hc := healthCheckJSON{
		P2P:           healthStatus{err: errors.New("not enough connected peers")},
		BeaconNode:    healthStatus{err: nil},
		ExecutionNode: healthStatus{err: nil},
		EventSyncer:   healthStatus{err: nil},
	}
	hc.Advanced.Peers = 3
	hc.Advanced.InboundConns = 3
	hc.Advanced.OutboundConns = 0
	hc.Advanced.ListenAddresses = []string{"127.0.0.1:8000"}

	s := hc.String()
	var result map[string]any

	require.NoError(t, json.Unmarshal([]byte(s), &result))
	require.Equal(t, "bad: not enough connected peers", result["p2p"])
	require.Equal(t, "good", result["beacon_node"])
	require.Equal(t, "good", result["execution_node"])
	require.Equal(t, "good", result["event_syncer"])

	advanced, ok := result["advanced"].(map[string]any)

	require.True(t, ok)
	require.Equal(t, float64(3), advanced["peers"])
	require.Equal(t, float64(3), advanced["inbound_conns"])
	require.Equal(t, float64(0), advanced["outbound_conns"])
	require.Equal(t, []any{"127.0.0.1:8000"}, advanced["p2p_listen_addresses"])
}
