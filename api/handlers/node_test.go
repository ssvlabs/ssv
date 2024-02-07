package handlers

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/bloxapp/ssv/api"
	p2pv1 "github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/nodeprobe"
	"github.com/bloxapp/ssv/utils/rsaencryption"
)

func TestHandlers(t *testing.T) {
	logger := zap.New(zapcore.NewNopCore(), zap.WithFatalHook(zapcore.WriteThenPanic))
	_, pv, err := rsaencryption.GenerateKeys()
	require.NoError(t, err)
	priv, err := rsaencryption.ConvertPemToPrivateKey(string(pv))
	require.NoError(t, err)
	n := 4
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	node := createTestNode(t, priv, n, ctx, logger)

	t.Run("identity", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/v1/node/identity", nil)
		require.NoError(t, err)
		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(api.Handler(node.Identity))
		handler.ServeHTTP(rr, req)
		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}
		var identity nodeIdentity
		err = json.Unmarshal(rr.Body.Bytes(), &identity)
		require.NoError(t, err)
		require.NotEmpty(t, identity.PeerID)
	})
	t.Run("peers", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/v1/node/peers", nil)
		require.NoError(t, err)
		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(api.Handler(node.Peers))
		handler.ServeHTTP(rr, req)
		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}
		var peers []peerInfo
		err = json.Unmarshal(rr.Body.Bytes(), &peers)
		require.NoError(t, err)
		require.Equal(t, 3, len(peers))
	})
	t.Run("sign", func(t *testing.T) {
		hash := sha256.Sum256([]byte("Hello"))
		data := []byte(fmt.Sprintf(`{"data":"%s"}`, hex.EncodeToString(hash[:])))
		r := bytes.NewReader(data)
		require.NoError(t, err)
		req, err := http.NewRequest("POST", "/v1/operator/sign", r)
		require.NoError(t, err)
		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(api.Handler(node.Sign))
		handler.ServeHTTP(rr, req)
		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}
		var sigResp struct {
			Signature string `json:"signature"`
		}
		err = json.Unmarshal(rr.Body.Bytes(), &sigResp)
		require.NoError(t, err)
		sigBytes, err := hex.DecodeString(sigResp.Signature)
		require.NoError(t, err)
		err = rsa.VerifyPKCS1v15(&priv.PublicKey, crypto.SHA256, hash[:], sigBytes)
		require.NoError(t, err)
	})
	t.Run("health", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/v1/node/health", nil)
		require.NoError(t, err)
		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(api.Handler(node.Health))
		handler.ServeHTTP(rr, req)
		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}
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
		err = json.Unmarshal(rr.Body.Bytes(), &health)
		require.NoError(t, err)
	})
	t.Run("topics", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/v1/node/topics", nil)
		require.NoError(t, err)
		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(api.Handler(node.Topics))
		handler.ServeHTTP(rr, req)
		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}
		var topics allPeersAndTopics
		err = json.Unmarshal(rr.Body.Bytes(), &topics)
		require.NoError(t, err)
	})
}

func createTestNode(t *testing.T, priv *rsa.PrivateKey, n int, ctx context.Context, logger *zap.Logger) *Node {
	pks := []string{"b768cdc2b2e0a859052bf04d1cd66383c96d95096a5287d08151494ce709556ba39c1300fbb902a0e2ebb7c31dc4e400",
		"824b9024767a01b56790a72afb5f18bb0f97d5bddb946a7bd8dd35cc607c35a4d76be21f24f484d0d478b99dc63ed170"}
	ln, routers, err := api.CreateNetworkAndSubscribe(t, ctx, p2pv1.LocalNetOptions{
		Nodes:        n,
		MinConnected: n/2 - 1,
		UseDiscv5:    false,
	}, pks...)
	require.NoError(t, err)
	require.NotNil(t, routers)
	require.NotNil(t, ln)

	node := &api.Node{}
	node.HealthyMock.Store(nil)
	nodeProber := nodeprobe.NewProber(zap.L(), nil, map[string]nodeprobe.Node{"consensus client": node, "execution client": node, "event syncer": node})
	return &Node{
		ListenAddresses: []string{fmt.Sprintf("tcp://%s:%d", "localhost", 3030), fmt.Sprintf("udp://%s:%d", "localhost", 3030)},
		PeersIndex:      ln.Nodes[0].(p2pv1.PeersIndexProvider).PeersIndex(),
		Network:         ln.Nodes[0].(p2pv1.HostProvider).Host().Network(),
		TopicIndex:      ln.Nodes[0].(TopicIndex),
		NodeProber:      nodeProber,
		Signer: func(data []byte) ([]byte, error) {
			return rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, data[:])
		},
	}
}
