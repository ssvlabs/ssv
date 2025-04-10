package handlers

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/network"
	p2pv1 "github.com/ssvlabs/ssv/network/p2p"
	"github.com/ssvlabs/ssv/nodeprobe"
)

// CreateTestNode builds a test Node using a local network.
func CreateTestNode(t *testing.T, n int, ctx context.Context) *Node {
	pks := []string{
		"b768cdc2b2e0a859052bf04d1cd66383c96d95096a5287d08151494ce709556ba39c1300fbb902a0e2ebb7c31dc4e400",
		"824b9024767a01b56790a72afb5f18bb0f97d5bddb946a7bd8dd35cc607c35a4d76be21f24f484d0d478b99dc63ed170",
	}
	ln, routers, err := createNetworkAndSubscribe(t, ctx, p2pv1.LocalNetOptions{
		Nodes:        n,
		MinConnected: n/2 - 1,
		UseDiscv5:    false,
	}, pks...)

	require.NoError(t, err)
	require.NotNil(t, routers)
	require.NotNil(t, ln)

	nodeMock := &NodeMock{}
	nodeMock.HealthyMock.Store(nil)
	nodeProber := nodeprobe.NewProber(zap.L(), nil, map[string]nodeprobe.Node{
		"consensus client": nodeMock,
		"execution client": nodeMock,
		"event syncer":     nodeMock,
	})

	return &Node{
		ListenAddresses: []string{
			fmt.Sprintf("tcp://%s:%d", "localhost", 3030),
			fmt.Sprintf("udp://%s:%d", "localhost", 3030),
		},
		PeersIndex: ln.Nodes[0].(p2pv1.PeersIndexProvider).PeersIndex(),
		Network:    ln.Nodes[0].(p2pv1.HostProvider).Host().Network(),
		TopicIndex: ln.Nodes[0].(TopicIndex),
		NodeProber: nodeProber,
	}
}

// createNetworkAndSubscribe creates a local network and subscribes each node to validator topics.
func createNetworkAndSubscribe(t *testing.T, ctx context.Context, options p2pv1.LocalNetOptions, pks ...string) (*p2pv1.LocalNet, []*dummyRouter, error) {
	logger, err := zap.NewDevelopment()

	require.NoError(t, err)

	ln, err := p2pv1.CreateAndStartLocalNet(ctx, logger.Named("createNetworkAndSubscribe"), options)

	if err != nil {
		return nil, nil, err
	}

	if len(ln.Nodes) != options.Nodes {
		return nil, nil, errors.Errorf("only %d peers created, expected %d", len(ln.Nodes), options.Nodes)
	}

	routers := make([]*dummyRouter, options.Nodes)

	for i, node := range ln.Nodes {
		routers[i] = &dummyRouter{i: i}
		node.UseMessageRouter(routers[i])
	}

	var wg sync.WaitGroup
	for _, pk := range pks {
		vpk, err := hex.DecodeString(pk)

		require.NoError(t, err, "failed to decode validator public key")

		for _, node := range ln.Nodes {
			wg.Add(1)

			go func(node network.P2PNetwork, vpk []byte) {
				defer wg.Done()
				_ = node.Subscribe(spectypes.ValidatorPK(vpk))
			}(node, vpk)
		}
	}
	wg.Wait()

	// allow time for subscriptions.
	time.Sleep(time.Second)
	for range pks {
		for _, node := range ln.Nodes {
			var peers []peer.ID
			for len(peers) < 2 {
				peers = node.Peers()
				time.Sleep(100 * time.Millisecond)
			}
		}
	}
	return ln, routers, nil
}

// dummyRouter is a dummy message router.
type dummyRouter struct {
	count uint64
	i     int
}

func (r *dummyRouter) Route(_ context.Context, _ network.DecodedSSVMessage) {
	atomic.AddUint64(&r.count, 1)
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
	n := 4
	ctx, cancel := context.WithCancel(context.Background())

	defer cancel()

	node := CreateTestNode(t, n, ctx)

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
	var result map[string]interface{}

	require.NoError(t, json.Unmarshal([]byte(s), &result))
	require.Equal(t, "bad: not enough connected peers", result["p2p"])
	require.Equal(t, "good", result["beacon_node"])
	require.Equal(t, "good", result["execution_node"])
	require.Equal(t, "good", result["event_syncer"])

	advanced, ok := result["advanced"].(map[string]interface{})

	require.True(t, ok)
	require.Equal(t, float64(3), advanced["peers"])
	require.Equal(t, float64(3), advanced["inbound_conns"])
	require.Equal(t, float64(0), advanced["outbound_conns"])
	require.Equal(t, []interface{}{"127.0.0.1:8000"}, advanced["p2p_listen_addresses"])
}
