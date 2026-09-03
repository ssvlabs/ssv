package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/hprobe"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/records"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

const (
	testOperatorID     = 7
	testOperatorPubKey = "LS0tLS1CRUdJTiBSU0EgUFVCTElDIEtFWS0tLS0t"
	testOwnerAddress   = "0x0000000000000000000000000000000000000abc"
	// hex of testDomainType, as the handler renders it
	testNetworkID = "0x00000503"
)

var testDomainType = spectypes.DomainType{0x0, 0x0, 0x5, 0x3}

// CreateTestNode builds a test Node using a local network.
func CreateTestNode(t *testing.T) *Node {
	componentMock := &ComponentMock{}
	componentMock.HealthyMock.Store(nil)
	healthProber := hprobe.NewHealthProber(zap.L())
	const component1 = "component_1"
	const component2 = "component_2"
	const component3 = "component_3"
	healthProber.AddComponent(component1, componentMock, 10*time.Second, 5, 0)
	healthProber.AddComponent(component2, componentMock, 10*time.Second, 5, 0)
	healthProber.AddComponent(component3, componentMock, 10*time.Second, 5, 0)

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

	opDataStore := &MockOperatorDataStore{
		OperatorDataValue: &registrystorage.OperatorData{
			ID:           testOperatorID,
			PublicKey:    testOperatorPubKey,
			OwnerAddress: common.HexToAddress(testOwnerAddress),
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
		opDataStore,
		func() spectypes.DomainType { return testDomainType },
		healthProber,
		component1,
		component2,
		component3,
	)
}

// ComponentMock is a dummy implementation of hprobe component.
type ComponentMock struct {
	HealthyMock atomic.Pointer[error]
}

func (nm *ComponentMock) Healthy(context.Context) error {
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
				require.Equal(t, testNetworkID, resp.NetworkID)
				require.Equal(t, spectypes.OperatorID(testOperatorID), resp.OperatorID)
				require.Equal(t, testOperatorPubKey, resp.OperatorPublicKey)
				require.Equal(t, common.HexToAddress(testOwnerAddress).String(), resp.OwnerAddress)
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

// TestPeers_SkipsNilMetadata verifies that the Peers and Health handlers don't
// panic when a peer's stored NodeInfo lacks a Metadata block. This happens in
// practice when a peer sends a NodeInfo envelope without the Metadata entry —
// we now reject such peers at handshake time, but historical entries in the
// index (or any future reader path) must not crash the API handler.
func TestPeers_SkipsNilMetadata(t *testing.T) {
	node := CreateTestNode(t)

	// Swap in a NodeInfo with Metadata == nil. CreateTestNode wires a
	// MockPeersIndex that returns the same NodeInfo for every peer ID, so
	// both connected peers end up looking like nil-Metadata peers.
	mockIdx, ok := node.peersIndex.(*MockPeersIndex)
	require.True(t, ok)
	mockIdx.nodeInfo = &records.NodeInfo{
		NetworkID: "mainnet",
		// Metadata intentionally left nil
	}

	t.Run("peers handler does not panic", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/v1/node/peers", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		api.Handler(node.Peers).ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)

		var peers []peerInfo
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &peers))
		// Peers with nil Metadata still appear in the response — only the
		// Version field is empty, since we skip the NodeVersion assignment.
		require.GreaterOrEqual(t, len(peers), 1)
		for _, p := range peers {
			require.Empty(t, p.Version, "expected empty version for nil-metadata peer")
		}
	})

	t.Run("health handler does not panic", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/v1/node/health", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		api.Handler(node.Health).ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
	})
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

// TestIdentity_OperatorIdentity covers the three states this node's operator identity
// can be in: registration synced, registration not yet synced, and a node running with
// no operator key at all. The distinction matters to a caller trying to confirm which
// operator a given node is - the public key answers that in every state, the id cannot.
func TestIdentity_OperatorIdentity(t *testing.T) {
	tests := []struct {
		name           string
		store          *MockOperatorDataStore
		wantPublicKey  string
		wantIdentified bool // operator_id and owner_address reported
	}{
		{
			name: "registration synced",
			store: &MockOperatorDataStore{
				OperatorDataValue: &registrystorage.OperatorData{
					ID:           testOperatorID,
					PublicKey:    testOperatorPubKey,
					OwnerAddress: common.HexToAddress(testOwnerAddress),
				},
			},
			wantPublicKey:  testOperatorPubKey,
			wantIdentified: true,
		},
		{
			// setupOperatorDataStore builds an OperatorData holding only the configured
			// public key when the operator is not found in storage, so the key is
			// available before the registration event has been synced - and the id is not.
			name: "registration not yet synced",
			store: &MockOperatorDataStore{
				OperatorDataValue: &registrystorage.OperatorData{PublicKey: testOperatorPubKey},
			},
			wantPublicKey:  testOperatorPubKey,
			wantIdentified: false,
		},
		{
			// A zero id is what makes an operator unidentified, not a missing owner
			// address: reading readiness separately from the data could pair an unsynced
			// snapshot with a raised ready flag and publish the zero address as real.
			name: "zero id is not identified even with an owner address",
			store: &MockOperatorDataStore{
				OperatorDataValue: &registrystorage.OperatorData{
					PublicKey:    testOperatorPubKey,
					OwnerAddress: common.HexToAddress(testOwnerAddress),
				},
			},
			wantPublicKey:  testOperatorPubKey,
			wantIdentified: false,
		},
		{
			// Exporter mode runs without an operator key and is given an empty
			// OperatorData, so it reports no operator identity at all.
			name:           "no operator key",
			store:          &MockOperatorDataStore{OperatorDataValue: &registrystorage.OperatorData{}},
			wantPublicKey:  "",
			wantIdentified: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := CreateTestNode(t)
			node.operatorDataStore = tt.store

			req, err := http.NewRequest("GET", "/v1/node/identity", nil)
			require.NoError(t, err)

			rr := httptest.NewRecorder()
			api.Handler(node.Identity).ServeHTTP(rr, req)
			require.Equal(t, http.StatusOK, rr.Code)

			var resp nodeIdentity
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))

			// decoded as a map too, so an omitted field is distinguishable from a zero one
			var raw map[string]any
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &raw))

			require.Equal(t, tt.wantPublicKey, resp.OperatorPublicKey)
			if tt.wantPublicKey == "" {
				require.NotContains(t, raw, "operator_public_key")
			}

			if tt.wantIdentified {
				require.Equal(t, spectypes.OperatorID(testOperatorID), resp.OperatorID)
				require.Equal(t, common.HexToAddress(testOwnerAddress).String(), resp.OwnerAddress)
			} else {
				require.NotContains(t, raw, "operator_id")
				require.NotContains(t, raw, "owner_address")
			}

			// the network id is independent of operator state and always reported
			require.Equal(t, testNetworkID, resp.NetworkID)
		})
	}
}
