package handlers

import (
	"bytes"
	"context"
	"crypto"
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
	node := CreateTestNode(t, priv, n, ctx, logger)

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
