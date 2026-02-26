package httpapi_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apiv1 "github.com/attestantio/go-builder-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/mev/builderendpoint/httpapi"
)

type fakeRegistrar struct {
	errs []string
	err  error
	got  int
}

func (r *fakeRegistrar) Forward(_ context.Context, registrations []*apiv1.SignedValidatorRegistration) ([]string, error) {
	r.got = len(registrations)
	return r.errs, r.err
}

func TestPostValidators_OK(t *testing.T) {
	t.Parallel()

	reg := &fakeRegistrar{}
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
	if reg.got != 0 {
		t.Fatalf("unexpected registrations: got %d want %d", reg.got, 0)
	}
}

func TestPostValidators_NoContentType_DefaultsToJSON(t *testing.T) {
	t.Parallel()

	reg := &fakeRegistrar{}
	handler := httpapi.NewRouter(zap.NewNop(), nil, nil, reg.Forward)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/eth/v1/builder/validators", strings.NewReader(`[]`))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	// Intentionally omit Content-Type for backwards compatibility.

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST validators: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d", resp.StatusCode, http.StatusOK)
	}
	if reg.got != 0 {
		t.Fatalf("unexpected registrations: got %d want %d", reg.got, 0)
	}
}

func TestPostValidators_BestEffortOnRelayErrors(t *testing.T) {
	t.Parallel()

	reg := &fakeRegistrar{errs: []string{"boom"}}
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

func TestPostValidators_UnsupportedContentType_Returns415(t *testing.T) {
	t.Parallel()

	reg := &fakeRegistrar{}
	handler := httpapi.NewRouter(zap.NewNop(), nil, nil, reg.Forward)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/eth/v1/builder/validators", "text/plain", strings.NewReader(`[]`))
	if err != nil {
		t.Fatalf("POST validators: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("unexpected status: got %d want %d", resp.StatusCode, http.StatusUnsupportedMediaType)
	}
}

func TestPostValidators_InvalidJSON_Returns400(t *testing.T) {
	t.Parallel()

	reg := &fakeRegistrar{}
	handler := httpapi.NewRouter(zap.NewNop(), nil, nil, reg.Forward)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/eth/v1/builder/validators", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST validators: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected status: got %d want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestPostValidators_SSZ_Returns200(t *testing.T) {
	t.Parallel()

	reg := &fakeRegistrar{}
	handler := httpapi.NewRouter(zap.NewNop(), nil, nil, reg.Forward)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	var pk phase0.BLSPubKey
	var sig phase0.BLSSignature
	var fee bellatrix.ExecutionAddress
	registration := &apiv1.SignedValidatorRegistration{
		Message: &apiv1.ValidatorRegistration{
			FeeRecipient: fee,
			GasLimit:     30_000_000,
			Timestamp:    time.Unix(0, 0),
			Pubkey:       pk,
		},
		Signature: sig,
	}
	regs := &apiv1.SignedValidatorRegistrations{Registrations: []*apiv1.SignedValidatorRegistration{registration}}
	body, err := regs.MarshalSSZTo(nil)
	if err != nil {
		t.Fatalf("marshal ssz: %v", err)
	}

	resp, err := http.Post(srv.URL+"/eth/v1/builder/validators", "application/octet-stream", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST validators: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d", resp.StatusCode, http.StatusOK)
	}
	if reg.got != 1 {
		t.Fatalf("unexpected registrations: got %d want %d", reg.got, 1)
	}
}
