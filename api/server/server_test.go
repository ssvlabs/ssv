package server

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/jwtauth/v5"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/bloxapp/ssv/api/handlers"
	"github.com/bloxapp/ssv/logging"
	operatorstorage "github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/rsaencryption"
)

func TestAPI(t *testing.T) {
	logger := zap.New(zapcore.NewNopCore(), zap.WithFatalHook(zapcore.WriteThenPanic))
	_, pv, err := rsaencryption.GenerateKeys()
	require.NoError(t, err)
	priv, err := rsaencryption.ConvertPemToPrivateKey(string(pv))
	require.NoError(t, err)
	n := 4
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	apiServer := createTestNodeWithAPI(t, priv, n, ctx, logger)
	router := apiServer.routes()
	testServer := httptest.NewServer(router)
	t.Run("authorized /v1/operator/sign", func(t *testing.T) {
		hash := sha256.Sum256([]byte("Hello"))
		data := []byte(fmt.Sprintf(`{"data":"%s"}`, hex.EncodeToString(hash[:])))
		r := bytes.NewReader(data)
		require.NoError(t, err)
		_, tokenString, err := jwtauth.New("HS256", []byte("secret"), nil).Encode(nil)
		require.NoError(t, err)
		resp, respData, err := handlers.TestRequest(testServer, "POST", "/v1/operator/sign", tokenString, r)
		require.NoError(t, err)
		require.Equal(t, resp.StatusCode, 200)
		var sigResp struct {
			Signature string `json:"signature"`
		}
		err = json.Unmarshal(respData, &sigResp)
		require.NoError(t, err)
		sigBytes, err := hex.DecodeString(sigResp.Signature)
		require.NoError(t, err)
		err = rsa.VerifyPKCS1v15(&priv.PublicKey, crypto.SHA256, hash[:], sigBytes)
		require.NoError(t, err)
	})
	t.Run("authorized /v1/operator/sign provided JWT token", func(t *testing.T) {
		hash := sha256.Sum256([]byte("Hello"))
		data := []byte(fmt.Sprintf(`{"data":"%s"}`, hex.EncodeToString(hash[:])))
		r := bytes.NewReader(data)
		require.NoError(t, err)
		tokenString := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.e30.t-IDcSemACt8x4iTMCda8Yhe3iZaWbvV5XKSTbuAn0M"
		resp, respData, err := handlers.TestRequest(testServer, "POST", "/v1/operator/sign", tokenString, r)
		require.NoError(t, err)
		require.Equal(t, resp.StatusCode, 200)
		var sigResp struct {
			Signature string `json:"signature"`
		}
		err = json.Unmarshal(respData, &sigResp)
		require.NoError(t, err)
		sigBytes, err := hex.DecodeString(sigResp.Signature)
		require.NoError(t, err)
		err = rsa.VerifyPKCS1v15(&priv.PublicKey, crypto.SHA256, hash[:], sigBytes)
		require.NoError(t, err)
	})
	t.Run("authorized /v1/operator/sign wrong JWT token", func(t *testing.T) {
		hash := sha256.Sum256([]byte("Hello"))
		data := []byte(fmt.Sprintf(`{"data":"%s"}`, hex.EncodeToString(hash[:])))
		r := bytes.NewReader(data)
		require.NoError(t, err)
		tokenString := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.e30.-h11M7Sw09EUpd-vqBIwAOMuIcogkBfpsYnIHVcVoEc"
		resp, _, err := handlers.TestRequest(testServer, "POST", "/v1/operator/sign", tokenString, r)
		require.NoError(t, err)
		require.Equal(t, resp.StatusCode, 401)
	})
	t.Run("non-authorized /v1/node/identity", func(t *testing.T) {
		_, respData, err := handlers.TestRequest(testServer, "GET", "/v1/node/identity", "", nil)
		require.NoError(t, err)
		var identity struct {
			PeerID    peer.ID  `json:"peer_id"`
			Addresses []string `json:"addresses"`
			Subnets   string   `json:"subnets"`
			Version   string   `json:"version"`
		}
		err = json.Unmarshal(respData, &identity)
		require.NoError(t, err)
		require.Equal(t, apiServer.node.Network.LocalPeer().String(), identity.PeerID.String())
	})
	t.Run("non-authorized /v1/node/peers", func(t *testing.T) {
		resp, respData, err := handlers.TestRequest(testServer, "GET", "/v1/node/peers", "", nil)
		require.NoError(t, err)
		require.Equal(t, resp.StatusCode, 200)
		var peers []struct {
			ID          peer.ID  `json:"id"`
			Addresses   []string `json:"addresses"`
			Connections []struct {
				Address   string `json:"address"`
				Direction string `json:"direction"`
			} `json:"connections"`
			Connectedness string `json:"connectedness"`
			Subnets       string `json:"subnets"`
			Version       string `json:"version"`
		}
		err = json.Unmarshal(respData, &peers)
		require.NoError(t, err)
	})
	t.Run("non-authorized /v1/node/topics", func(t *testing.T) {
		resp, respData, err := handlers.TestRequest(testServer, "GET", "/v1/node/topics", "", nil)
		require.NoError(t, err)
		require.Equal(t, resp.StatusCode, 200)
		var topics struct {
			AllPeers     []peer.ID `json:"all_peers"`
			PeersByTopic []struct {
				TopicName string    `json:"topic"`
				Peers     []peer.ID `json:"peers"`
			} `json:"peers_by_topic"`
		}
		err = json.Unmarshal(respData, &topics)
		require.NoError(t, err)
	})
	t.Run("non-authorized /v1/node/health", func(t *testing.T) {
		resp, respData, err := handlers.TestRequest(testServer, "GET", "/v1/node/health", "", nil)
		require.NoError(t, err)
		require.Equal(t, resp.StatusCode, 200)
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
		err = json.Unmarshal(respData, &health)
		require.NoError(t, err)
	})
}

func createTestNodeWithAPI(t *testing.T, priv *rsa.PrivateKey, n int, ctx context.Context, logger *zap.Logger) *Server {
	node := handlers.CreateTestNode(t, priv, n, ctx, logger)
	db, err := kv.NewInMemory(logging.TestLogger(t), basedb.Options{
		Reporting: true,
		Ctx:       context.Background(),
		Path:      t.TempDir(),
	})
	require.NoError(t, err)
	nodeStorage, err := operatorstorage.NewNodeStorage(logger, db)
	require.NoError(t, err)
	return New(
		logger,
		fmt.Sprintf(":%d", 3000),
		node,
		&handlers.Validators{
			Shares: nodeStorage.Shares(),
		},
		jwtauth.New("HS256", []byte("secret"), nil),
	)
}
