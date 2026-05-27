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
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/api"
	hexporter "github.com/ssvlabs/ssv/api/handlers/exporter"
	hnode "github.com/ssvlabs/ssv/api/handlers/node"
	hvalidators "github.com/ssvlabs/ssv/api/handlers/validators"
	"github.com/ssvlabs/ssv/utils/commons"
)

// setupTestServer creates and configures a test HTTP server with mock handlers.
func setupTestServer(t *testing.T) *httptest.Server {
	router := chi.NewRouter()

	router.Use(middlewareRecover(zaptest.NewLogger(t)))
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
		&hnode.Node{},
		&hvalidators.Validators{},
		&hexporter.Exporter{},
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
	require.NoError(t, conn.Close())

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
		&hnode.Node{},
		&hvalidators.Validators{},
		&hexporter.Exporter{},
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

// TestMiddlewareRecover verifies that middlewareRecover catches panics from a
// downstream handler, logs them with structured fields at ERROR level, and
// returns a 500 to the client. The companion sub-test confirms that
// http.ErrAbortHandler propagates unchanged (matching chi's behavior).
func TestMiddlewareRecover(t *testing.T) {
	t.Parallel()

	t.Run("captures panic and returns 500", func(t *testing.T) {
		core, observed := observer.New(zapcore.ErrorLevel)
		logger := zap.New(core)

		handler := middlewareRecover(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("boom")
		}))

		req := httptest.NewRequest("GET", "/v1/node/peers", nil)
		req.RemoteAddr = "1.2.3.4:5678"
		rr := httptest.NewRecorder()

		require.NotPanics(t, func() {
			handler.ServeHTTP(rr, req)
		})
		require.Equal(t, http.StatusInternalServerError, rr.Code)

		require.Equal(t, 1, observed.Len())
		entry := observed.All()[0]
		require.Equal(t, "panic serving SSV API request", entry.Message)

		fields := entry.ContextMap()
		require.Equal(t, "GET", fields["method"])
		require.Equal(t, "/v1/node/peers", fields["path"])
		require.Equal(t, "1.2.3.4:5678", fields["remote_addr"])
		require.Equal(t, "boom", fields["panic"])
		require.Contains(t, fields, "stack")
	})

	t.Run("rethrows http.ErrAbortHandler", func(t *testing.T) {
		logger := zap.NewNop()

		handler := middlewareRecover(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic(http.ErrAbortHandler)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		rr := httptest.NewRecorder()

		require.PanicsWithValue(t, http.ErrAbortHandler, func() {
			handler.ServeHTTP(rr, req)
		})
	})

	t.Run("passthrough on no panic", func(t *testing.T) {
		core, observed := observer.New(zapcore.ErrorLevel)
		logger := zap.New(core)

		handler := middlewareRecover(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)
		require.Equal(t, "ok", rr.Body.String())
		require.Equal(t, 0, observed.Len(), "no panic should produce no log")
	})
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
