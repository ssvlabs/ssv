package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestMiddlewareLoggerSkipsFastSuccessfulRequests(t *testing.T) {
	t.Parallel()

	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core)
	handler := middlewareLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if logs.Len() != 0 {
		t.Fatalf("expected no logs, got %d", logs.Len())
	}
}

func TestMiddlewareLoggerLogsSlowSuccessfulRequests(t *testing.T) {
	t.Parallel()

	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core)
	handler := middlewareLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(3 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/slow", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if logs.Len() != 1 {
		t.Fatalf("expected 1 log, got %d", logs.Len())
	}
	if got := logs.All()[0].ContextMap()["status"]; got != int64(http.StatusOK) {
		t.Fatalf("unexpected status: got %v want %d", got, http.StatusOK)
	}
}

func TestMiddlewareLoggerLogsErrorResponses(t *testing.T) {
	t.Parallel()

	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core)
	handler := middlewareLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))

	req := httptest.NewRequest(http.MethodGet, "/error", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if logs.Len() != 1 {
		t.Fatalf("expected 1 log, got %d", logs.Len())
	}
	if got := logs.All()[0].ContextMap()["status"]; got != int64(http.StatusBadGateway) {
		t.Fatalf("unexpected status: got %v want %d", got, http.StatusBadGateway)
	}
}
