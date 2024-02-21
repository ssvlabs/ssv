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
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/jwtauth/v5"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/bloxapp/ssv/api/handlers"
	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/message/validation"
	"github.com/bloxapp/ssv/network"
	p2pv1 "github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/nodeprobe"
	operatorstorage "github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
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
		resp, respData, err := testRequest(testServer, "POST", "/v1/operator/sign", tokenString, r)
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
		resp, respData, err := testRequest(testServer, "POST", "/v1/operator/sign", tokenString, r)
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
		resp, _, err := testRequest(testServer, "POST", "/v1/operator/sign", tokenString, r)
		require.NoError(t, err)
		require.Equal(t, resp.StatusCode, 401)
	})
	t.Run("non-authorized /v1/node/identity", func(t *testing.T) {
		_, respData, err := testRequest(testServer, "GET", "/v1/node/identity", "", nil)
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
		resp, respData, err := testRequest(testServer, "GET", "/v1/node/peers", "", nil)
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
		resp, respData, err := testRequest(testServer, "GET", "/v1/node/topics", "", nil)
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
}

func createTestNodeWithAPI(t *testing.T, priv *rsa.PrivateKey, n int, ctx context.Context, logger *zap.Logger) *Server {
	pks := []string{"b768cdc2b2e0a859052bf04d1cd66383c96d95096a5287d08151494ce709556ba39c1300fbb902a0e2ebb7c31dc4e400",
		"824b9024767a01b56790a72afb5f18bb0f97d5bddb946a7bd8dd35cc607c35a4d76be21f24f484d0d478b99dc63ed170"}
	ln, routers, err := createNetworkAndSubscribe(t, ctx, p2pv1.LocalNetOptions{
		Nodes:        n,
		MinConnected: n/2 - 1,
		UseDiscv5:    false,
	}, pks...)
	require.NoError(t, err)
	require.NotNil(t, routers)
	require.NotNil(t, ln)

	node := &node{}
	node.healthy.Store(nil)
	nodeProber := nodeprobe.NewProber(zap.L(), nil, map[string]nodeprobe.Node{"test node": node})
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

	logger.Debug("created local network")

	routers := make([]*dummyRouter, options.Nodes)
	for i, node := range ln.Nodes {
		routers[i] = &dummyRouter{
			i: i,
		}
		node.UseMessageRouter(routers[i])
	}

	logger.Debug("subscribing to topics")

	var wg sync.WaitGroup
	for _, pk := range pks {
		vpk, err := hex.DecodeString(pk)
		if err != nil {
			return nil, nil, errors.Wrap(err, "could not decode validator public key")
		}
		for _, node := range ln.Nodes {
			wg.Add(1)
			go func(node network.P2PNetwork, vpk []byte) {
				defer wg.Done()
				if err := node.Subscribe(vpk); err != nil {
					logger.Warn("could not subscribe to topic", zap.Error(err))
				}
			}(node, vpk)
		}
	}
	wg.Wait()
	// let the nodes subscribe
	<-time.After(time.Second)
	for _, pk := range pks {
		vpk, err := hex.DecodeString(pk)
		if err != nil {
			return nil, nil, errors.Wrap(err, "could not decode validator public key")
		}
		for _, node := range ln.Nodes {
			peers := make([]peer.ID, 0)
			for len(peers) < 2 {
				peers, err = node.Peers(vpk)
				if err != nil {
					return nil, nil, err
				}
				time.Sleep(time.Millisecond * 100)
			}
		}
	}

	return ln, routers, nil
}

type LocalNetOptions struct {
	MessageValidatorProvider                        func(int) validation.MessageValidator
	Nodes                                           int
	MinConnected                                    int
	UseDiscv5                                       bool
	TotalValidators, ActiveValidators, MyValidators int
	PeerScoreInspector                              func(selfPeer peer.ID, peerMap map[peer.ID]*pubsub.PeerScoreSnapshot)
	PeerScoreInspectorInterval                      time.Duration
}

type dummyRouter struct {
	count uint64
	i     int
}

func (r *dummyRouter) Route(_ context.Context, _ *queue.DecodedSSVMessage) {
	atomic.AddUint64(&r.count, 1)
}

type node struct {
	healthy atomic.Pointer[error]
}

func (sc *node) Healthy(context.Context) error {
	err := sc.healthy.Load()
	if err != nil {
		return *err
	}
	return nil
}

func testRequest(ts *httptest.Server, method, path, token string, body io.Reader) (*http.Response, []byte, error) {
	req, err := http.NewRequest(method, ts.URL+path, body)
	if err != nil {
		return nil, nil, err
	}
	if token != "" {
		req.Header = http.Header{
			"Content-Type":  {"application/json"},
			"Authorization": {"Bearer " + token},
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	return resp, respBody, nil
}
