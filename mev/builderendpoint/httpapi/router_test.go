package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	builderapi "github.com/attestantio/go-builder-client/api/deneb"
	builderspec "github.com/attestantio/go-builder-client/spec"
	consensusspec "github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/holiman/uint256"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/mev/builderendpoint/domain"
	"github.com/ssvlabs/ssv/mev/builderendpoint/httpapi"
)

func TestStatusEndpoint(t *testing.T) {
	t.Parallel()

	handler := httpapi.NewRouter(httpapi.Dependencies{
		Logger:      zap.NewNop(),
		BidProvider: domain.NoopBidProvider{},
	})
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

type fakeBidProvider struct {
	bid *builderspec.VersionedSignedBuilderBid
	err error
}

func (p fakeBidProvider) BuilderBid(_ context.Context, _ phase0.Slot, _ phase0.Hash32, _ phase0.BLSPubKey) (*builderspec.VersionedSignedBuilderBid, error) {
	return p.bid, p.err
}

func TestGetHeader_InvalidParams(t *testing.T) {
	t.Parallel()

	handler := httpapi.NewRouter(httpapi.Dependencies{
		Logger:      zap.NewNop(),
		BidProvider: fakeBidProvider{},
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	tests := []string{
		"/eth/v1/builder/header/not-a-slot/0x" + hex32() + "/0x" + hex48(),
		"/eth/v1/builder/header/1/0xdeadbeef/0x" + hex48(),
		"/eth/v1/builder/header/1/0x" + hex32() + "/0xdeadbeef",
	}

	for _, path := range tests {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("GET %s: got %d want %d", path, resp.StatusCode, http.StatusBadRequest)
		}
	}
}

func TestGetHeader_NoBid_Returns204(t *testing.T) {
	t.Parallel()

	handler := httpapi.NewRouter(httpapi.Dependencies{
		Logger:      zap.NewNop(),
		BidProvider: fakeBidProvider{bid: nil, err: nil},
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	path := "/eth/v1/builder/header/1/0x" + hex32() + "/0x" + hex48()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET header: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("unexpected status code: got %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
}

func TestGetHeader_Bid_Returns200AndConsensusHeader(t *testing.T) {
	t.Parallel()

	bid := &builderspec.VersionedSignedBuilderBid{
		Version: consensusspec.DataVersionDeneb,
		Deneb: &builderapi.SignedBuilderBid{
			Message: &builderapi.BuilderBid{
				Header: &deneb.ExecutionPayloadHeader{BaseFeePerGas: uint256.NewInt(0)},
				Value:  uint256.NewInt(0),
			},
		},
	}

	handler := httpapi.NewRouter(httpapi.Dependencies{
		Logger:      zap.NewNop(),
		BidProvider: fakeBidProvider{bid: bid, err: nil},
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	path := "/eth/v1/builder/header/1/0x" + hex32() + "/0x" + hex48()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET header: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code: got %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if got := resp.Header.Get(httpapi.EthConsensusVersion); got != "deneb" {
		t.Fatalf("unexpected %s: got %q want %q", httpapi.EthConsensusVersion, got, "deneb")
	}
}

func hex32() string { return "0000000000000000000000000000000000000000000000000000000000000000" }
func hex48() string {
	return "000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"
}
