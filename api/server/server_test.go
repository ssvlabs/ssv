package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	crand "crypto/rand"
	"encoding/json"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/api/handlers"
	"github.com/ssvlabs/ssv/utils/commons"
)

// setupTestServer creates and configures a test HTTP server with mock handlers.
func setupTestServer(t *testing.T) *httptest.Server {
	router := chi.NewRouter()

	router.Use(middleware.Recoverer)
	router.Use(middleware.Throttle(runtime.NumCPU() * 4))
	router.Use(middleware.Compress(5, "application/json"))
	router.Use(middlewareLogger(zaptest.NewLogger(t)))
	router.Use(middlewareNodeVersion)

	nodeIdentityHandler := func(w http.ResponseWriter, r *http.Request) {
		err := api.Render(w, r, map[string]interface{}{
			"peer_id":   "test-node-id",
			"addresses": []string{"test-address"},
			"subnets":   "test-subnets",
			"version":   "test-version",
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}

	nodePeersHandler := func(w http.ResponseWriter, r *http.Request) {
		err := api.Render(w, r, []map[string]interface{}{
			{"id": "peer1", "addresses": []string{"addr1"}},
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}

	nodeTopicsHandler := func(w http.ResponseWriter, r *http.Request) {
		err := api.Render(w, r, map[string]interface{}{
			"all_peers": []string{"peer1", "peer2"},
			"peers_by_topic": []map[string]interface{}{
				{
					"topic": "topic1",
					"peers": []string{"peer1"},
				},
			},
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}

	nodeHealthHandler := func(w http.ResponseWriter, r *http.Request) {
		err := api.Render(w, r, map[string]interface{}{
			"p2p":            "good",
			"beacon_node":    "good",
			"execution_node": "good",
			"event_syncer":   "good",
			"advanced": map[string]interface{}{
				"peers":                2,
				"inbound_conns":        1,
				"outbound_conns":       1,
				"p2p_listen_addresses": []string{"addr1"},
			},
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}

	validatorsListHandler := func(w http.ResponseWriter, r *http.Request) {
		err := api.Render(w, r, map[string]interface{}{
			"data": []map[string]interface{}{
				{"validator": "1", "pubkey": "0x123"},
			},
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}

	exporterDecidedsHandler := func(w http.ResponseWriter, r *http.Request) {
		err := api.Render(w, r, map[string]interface{}{
			"data": []map[string]interface{}{
				{"slot": 1, "role": "attester"},
			},
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}

	router.Get("/v1/node/identity", nodeIdentityHandler)
	router.Get("/v1/node/peers", nodePeersHandler)
	router.Get("/v1/node/topics", nodeTopicsHandler)
	router.Get("/v1/node/health", nodeHealthHandler)
	router.Get("/v1/validators", validatorsListHandler)
	router.Get("/v1/exporter/decideds", exporterDecidedsHandler)
	router.Post("/v1/exporter/decideds", exporterDecidedsHandler)

	return httptest.NewServer(router)
}

// TestNew tests the New constructor function.
func TestNew(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	node := &handlers.Node{}
	validators := &handlers.Validators{}
	exporter := &handlers.Exporter{}

	server := New(
		logger,
		":8080",
		node,
		nil,
		validators,
		exporter,
		false,
	)

	require.NotNil(t, server)
	require.Equal(t, logger, server.logger)
	require.Equal(t, ":8080", server.addr)
	require.Equal(t, node, server.node)
	require.Equal(t, validators, server.validators)
	require.Equal(t, exporter, server.exporter)
}

// TestRun_ActualExecution tests that the Run method starts a server.
func TestRun_ActualExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	listener, err := net.Listen("tcp", "localhost:0")

	require.NoError(t, err)

	port := listener.Addr().(*net.TCPAddr).Port
	addr := fmt.Sprintf("localhost:%d", port)

	err = listener.Close()

	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	srv := New(
		logger,
		addr,
		&handlers.Node{},
		nil,
		&handlers.Validators{},
		&handlers.Exporter{},
		false,
	)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run()
	}()

	var conn net.Conn
	var connectErr error

	for i := 0; i < 10; i++ {
		conn, connectErr = net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if connectErr == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	require.NoError(t, connectErr, "failed to connect to server after multiple attempts")

	conn.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	err = srv.httpServer.Shutdown(ctx)
	if err != nil {
		t.Logf("error shutting down server: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) && !strings.Contains(err.Error(), "closed") {
			t.Logf("server exited with unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not exit in time")
	}
}

// TestRun_ActualExecutionFullMode tests that the Run method starts a server in full exporter mode.
func TestRun_ActualExecutionFullMode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	listener, err := net.Listen("tcp", "localhost:0")

	require.NoError(t, err)

	port := listener.Addr().(*net.TCPAddr).Port
	addr := fmt.Sprintf("localhost:%d", port)

	err = listener.Close()

	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	srv := New(
		logger,
		addr,
		&handlers.Node{},
		nil,
		&handlers.Validators{},
		&handlers.Exporter{},
		true,
	)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run()
	}()

	var conn net.Conn
	var connectErr error

	for range 10 {
		conn, connectErr = net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if connectErr == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	require.NoError(t, connectErr, "failed to connect to server after multiple attempts")

	conn.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	err = srv.httpServer.Shutdown(ctx)
	if err != nil {
		t.Logf("error shutting down server: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) && !strings.Contains(err.Error(), "closed") {
			t.Logf("server exited with unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not exit in time")
	}
}

// TestMiddlewareLogger tests the logger middleware.
func TestMiddlewareLogger(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	m := middlewareLogger(logger)

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("test"))
	})

	handler := m(nextHandler)

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "test", w.Body.String())
}

// TestMiddlewareNodeVersion tests the node version middleware.
func TestMiddlewareNodeVersion(t *testing.T) {
	t.Parallel()

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := middlewareNodeVersion(nextHandler)

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	require.Equal(t, commons.GetNodeVersion(), w.Header().Get("X-SSV-Node-Version"))
}

// TestRoutes checks that all routes are registered correctly.
func TestRoutes(t *testing.T) {
	t.Parallel()

	ts := setupTestServer(t)
	defer ts.Close()

	routes := []struct {
		method       string
		path         string
		expectedCode int
		validateBody func(t *testing.T, body string)
	}{
		{
			method:       "GET",
			path:         "/v1/node/identity",
			expectedCode: http.StatusOK,
			validateBody: func(t *testing.T, body string) {
				require.Contains(t, body, "test-node-id")
			},
		},
		{
			method:       "GET",
			path:         "/v1/node/peers",
			expectedCode: http.StatusOK,
			validateBody: func(t *testing.T, body string) {
				require.Contains(t, body, "peer1")
			},
		},
		{
			method:       "GET",
			path:         "/v1/node/topics",
			expectedCode: http.StatusOK,
			validateBody: func(t *testing.T, body string) {
				require.Contains(t, body, "topic")
			},
		},
		{
			method:       "GET",
			path:         "/v1/node/health",
			expectedCode: http.StatusOK,
			validateBody: func(t *testing.T, body string) {
				require.Contains(t, body, "good")
			},
		},
		{
			method:       "GET",
			path:         "/v1/validators",
			expectedCode: http.StatusOK,
			validateBody: func(t *testing.T, body string) {
				require.Contains(t, body, "validator")
			},
		},
		{
			method:       "GET",
			path:         "/v1/exporter/decideds",
			expectedCode: http.StatusOK,
			validateBody: func(t *testing.T, body string) {
				require.Contains(t, body, "slot")
			},
		},
		{
			method:       "POST",
			path:         "/v1/exporter/decideds",
			expectedCode: http.StatusOK,
			validateBody: func(t *testing.T, body string) {
				require.Contains(t, body, "data")
			},
		},
	}

	for _, route := range routes {
		t.Run(fmt.Sprintf("%s %s", route.method, route.path), func(t *testing.T) {
			url := fmt.Sprintf("%s%s", ts.URL, route.path)
			req, err := http.NewRequest(route.method, url, nil)
			require.NoError(t, err)

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)

			require.NoError(t, err)

			require.Equal(t, route.expectedCode, resp.StatusCode, "Unexpected status code")

			route.validateBody(t, string(body))
		})
	}
}

// --- Pinned peers routes ---

// mockPinnedProviderServer implements handlers.PinnedPeersProvider for server tests.
type mockPinnedProviderServer struct {
	pinned     []peer.AddrInfo
	pinCalls   []peer.AddrInfo
	unpinCalls []peer.ID
}

func (m *mockPinnedProviderServer) ListPinned() []peer.AddrInfo {
	out := make([]peer.AddrInfo, len(m.pinned))
	copy(out, m.pinned)
	return out
}

func (m *mockPinnedProviderServer) PinPeer(ai peer.AddrInfo) error {
	m.pinCalls = append(m.pinCalls, ai)
	for _, p := range m.pinned {
		if p.ID == ai.ID {
			return nil
		}
	}
	m.pinned = append(m.pinned, ai)
	return nil
}

func (m *mockPinnedProviderServer) UnpinPeer(id peer.ID) error {
	m.unpinCalls = append(m.unpinCalls, id)
	kept := make([]peer.AddrInfo, 0, len(m.pinned))
	for _, p := range m.pinned {
		if p.ID != id {
			kept = append(kept, p)
		}
	}
	m.pinned = kept
	return nil
}

type mockConnCheckerServer struct{}

func (mockConnCheckerServer) IsConnected(peer.ID) bool { return false }

func genPeerID(t *testing.T) peer.ID {
	t.Helper()
	sk, _, err := libp2pcrypto.GenerateEd25519Key(crand.Reader)
	require.NoError(t, err)
	id, err := peer.IDFromPrivateKey(sk)
	require.NoError(t, err)
	return id
}

func maFor(id peer.ID) string { return "/ip4/127.0.0.1/tcp/13001/p2p/" + id.String() }

func TestRun_WithPinnedPeersEndpoints(t *testing.T) {
	// pick a free localhost port
	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	addr := fmt.Sprintf("localhost:%d", port)
	require.NoError(t, listener.Close())

	prov := &mockPinnedProviderServer{}
	id1 := genPeerID(t)
	addr1, _ := ma.NewMultiaddr(maFor(id1))
	prov.pinned = []peer.AddrInfo{{ID: id1, Addrs: []ma.Multiaddr{addr1}}}

	srv := New(
		zaptest.NewLogger(t),
		addr,
		&handlers.Node{},
		&handlers.PinnedP2PPeers{Provider: prov, Conn: mockConnCheckerServer{}},
		&handlers.Validators{},
		&handlers.Exporter{},
		false,
	)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run() }()

	// wait for server to accept connections
	var connectErr error
	for i := 0; i < 10; i++ {
		_, connectErr = net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if connectErr == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.NoError(t, connectErr)

	baseURL := "http://" + addr
	// GET list pinned peers
	resp, err := http.Get(baseURL + "/v1/node/pinned-peers")
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, string(body), id1.String())

	// POST add another peer
	id2 := genPeerID(t)
	addr2 := maFor(id2)
	addReq := handlers.PinPeersRequest{Peers: []string{addr2}}
	b, _ := json.Marshal(addReq)
	resp, err = http.Post(baseURL+"/v1/node/pinned-peers", "application/json", strings.NewReader(string(b)))
	require.NoError(t, err)
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, string(body), id2.String())

	// DELETE remove id1
	delReq := handlers.UnpinPeersRequest{Peers: []string{maFor(id1)}}
	b, _ = json.Marshal(delReq)
	httpReq, _ := http.NewRequest(http.MethodDelete, baseURL+"/v1/node/pinned-peers", strings.NewReader(string(b)))
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(httpReq)
	require.NoError(t, err)
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, string(body), id1.String())

	// shutdown server
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_ = srv.httpServer.Shutdown(ctx)
	select {
	case <-time.After(2 * time.Second):
		t.Fatal("server did not exit in time")
	case <-errCh:
		// ok
	}
}
