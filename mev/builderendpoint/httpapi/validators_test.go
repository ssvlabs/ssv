package httpapi_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/mev/builderendpoint/httpapi"
)

type fakeRegistrar struct {
	errs []string
	err  error
}

func (r fakeRegistrar) Forward(context.Context, io.ReadCloser) ([]string, error) {
	return r.errs, r.err
}

func TestPostValidators_OK(t *testing.T) {
	t.Parallel()

	reg := fakeRegistrar{}
	handler := httpapi.NewRouter(zap.NewNop(), nil, nil, reg.Forward)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/eth/v1/builder/validators", "application/json", strings.NewReader(`[]`))
	if err != nil {
		t.Fatalf("POST validators: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestPostValidators_BadRequestOnErrors(t *testing.T) {
	t.Parallel()

	reg := fakeRegistrar{errs: []string{"boom"}}
	handler := httpapi.NewRouter(zap.NewNop(), nil, nil, reg.Forward)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/eth/v1/builder/validators", "application/json", strings.NewReader(`[]`))
	if err != nil {
		t.Fatalf("POST validators: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected status: got %d want %d", resp.StatusCode, http.StatusBadRequest)
	}
}
