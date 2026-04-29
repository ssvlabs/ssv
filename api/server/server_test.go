package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/ssvlabs/ssv/api"
	hexporter "github.com/ssvlabs/ssv/api/handlers/exporter"
	hnode "github.com/ssvlabs/ssv/api/handlers/node"
	hvalidators "github.com/ssvlabs/ssv/api/handlers/validators"
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
		err := api.Render(w, r, map[string]any{
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
		err := api.Render(w, r, []map[string]any{
			{"id": "peer1", "addresses": []string{"addr1"}},
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}

	nodeTopicsHandler := func(w http.ResponseWriter, r *http.Request) {
		err := api.Render(w, r, map[string]any{
			"all_peers": []string{"peer1", "peer2"},
			"peers_by_topic": []map[string]any{
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
		err := api.Render(w, r, map[string]any{
			"p2p":            "good",
			"beacon_node":    "good",
			"execution_node": "good",
			"event_syncer":   "good",
			"advanced": map[string]any{
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
		err := api.Render(w, r, map[string]any{
			"data": []map[string]any{
				{"validator": "1", "pubkey": "0x123"},
			},
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}

	exporterDecidedsHandler := func(w http.ResponseWriter, r *http.Request) {
		err := api.Render(w, r, map[string]any{
			"data": []map[string]any{
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
	node := &hnode.Node{}
	validators := &hvalidators.Validators{}
	exporter := &hexporter.Exporter{}

	server := New(
		logger,
		":8080",
		node,
		validators,
		exporter,
		false,
	)

	require.NotNil(t, server)
	require.Equal(t, logger, server.logger)
	require.Equal(t, ":8080", server.addr)
}

// TestStart_ActualExecution verifies Start binds a listener, serves the API,
// and shuts down cleanly when the context is canceled.
func TestStart_ActualExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	srv := New(
		zaptest.NewLogger(t),
		"127.0.0.1:0",
		&hnode.Node{},
		&hvalidators.Validators{},
		&hexporter.Exporter{},
		false,
	)

	addr, _, err := srv.Start(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, addr)

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	cancel()

	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err != nil {
			return true
		}
		_ = conn.Close()
		return false
	}, 3*time.Second, 50*time.Millisecond, "server did not stop accepting connections after ctx cancel")
}

// TestStart_ActualExecutionFullMode verifies Start works in full exporter mode.
func TestStart_ActualExecutionFullMode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	srv := New(
		zaptest.NewLogger(t),
		"127.0.0.1:0",
		&hnode.Node{},
		&hvalidators.Validators{},
		&hexporter.Exporter{},
		true,
	)

	addr, _, err := srv.Start(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, addr)

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	cancel()

	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err != nil {
			return true
		}
		_ = conn.Close()
		return false
	}, 3*time.Second, 50*time.Millisecond, "server did not stop accepting connections after ctx cancel")
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
