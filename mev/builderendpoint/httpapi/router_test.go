package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/mev/builderendpoint/httpapi"
)

func TestStatusEndpoint(t *testing.T) {
	t.Parallel()

	handler := httpapi.NewRouter(zap.NewNop())
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/eth/v1/builder/status")
	if err != nil {
		t.Fatalf("GET status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
}
