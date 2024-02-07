package server

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
	"net/http/httptest"
	"testing"

	"github.com/go-chi/jwtauth/v5"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/bloxapp/ssv/api"
	"github.com/bloxapp/ssv/api/handlers"
	"github.com/bloxapp/ssv/logging"
	p2pv1 "github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/nodeprobe"
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
		resp, respData, err := api.TestRequest(testServer, "POST", "/v1/operator/sign", tokenString, r)
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
		resp, respData, err := api.TestRequest(testServer, "POST", "/v1/operator/sign", tokenString, r)
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
		resp, _, err := api.TestRequest(testServer, "POST", "/v1/operator/sign", tokenString, r)
		require.NoError(t, err)
		require.Equal(t, resp.StatusCode, 401)
	})
	t.Run("non-authorized /v1/node/identity", func(t *testing.T) {
		_, respData, err := api.TestRequest(testServer, "GET", "/v1/node/identity", "", nil)
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
		resp, respData, err := api.TestRequest(testServer, "GET", "/v1/node/peers", "", nil)
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
		resp, respData, err := api.TestRequest(testServer, "GET", "/v1/node/topics", "", nil)
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
		resp, respData, err := api.TestRequest(testServer, "GET", "/v1/node/health", "", nil)
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
		&handlers.Node{
			ListenAddresses: []string{fmt.Sprintf("tcp://%s:%d", "localhost", 3030), fmt.Sprintf("udp://%s:%d", "localhost", 3030)},
			PeersIndex:      ln.Nodes[0].(p2pv1.PeersIndexProvider).PeersIndex(),
			Network:         ln.Nodes[0].(p2pv1.HostProvider).Host().Network(),
			TopicIndex:      ln.Nodes[0].(handlers.TopicIndex),
			NodeProber:      nodeProber,
			Signer: func(data []byte) ([]byte, error) {
				return rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, data[:])
			},
		},
		&handlers.Validators{
			Shares: nodeStorage.Shares(),
		},
		jwtauth.New("HS256", []byte("secret"), nil),
	)
}
