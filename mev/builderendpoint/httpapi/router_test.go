package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	builderapi "github.com/attestantio/go-builder-client/api/deneb"
	builderspec "github.com/attestantio/go-builder-client/spec"
	"github.com/attestantio/go-eth2-client/api"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	consensusspec "github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/holiman/uint256"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/mev/builderendpoint/httpapi"
	buildercodec "github.com/ssvlabs/ssv/mev/builderendpoint/httpapi/codec"
)

func TestStatusEndpoint(t *testing.T) {
	t.Parallel()

	handler := httpapi.NewRouter(zap.NewNop(), nil, nil, nil)
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

func (p fakeBidProvider) Bid(_ context.Context, _ phase0.Slot, _ phase0.Hash32, _ phase0.BLSPubKey) (*builderspec.VersionedSignedBuilderBid, error) {
	return p.bid, p.err
}

func TestGetHeader_InvalidParams(t *testing.T) {
	t.Parallel()

	p := fakeBidProvider{}
	handler := httpapi.NewRouter(zap.NewNop(), p.Bid, nil, nil)
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

	p := fakeBidProvider{bid: nil, err: nil}
	handler := httpapi.NewRouter(zap.NewNop(), p.Bid, nil, nil)
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

	p := fakeBidProvider{bid: bid, err: nil}
	handler := httpapi.NewRouter(zap.NewNop(), p.Bid, nil, nil)
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

func TestGetHeader_Bid_DefaultJSON_HasEnvelope(t *testing.T) {
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

	p := fakeBidProvider{bid: bid, err: nil}
	handler := httpapi.NewRouter(zap.NewNop(), p.Bid, nil, nil)
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
	if got := resp.Header.Get("Content-Type"); got != buildercodec.MediaTypeJSON {
		t.Fatalf("unexpected Content-Type: got %q want %q", got, buildercodec.MediaTypeJSON)
	}

	var decoded map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := decoded["version"]; !ok {
		t.Fatalf("missing top-level version")
	}
	if _, ok := decoded["data"]; !ok {
		t.Fatalf("missing top-level data")
	}
}

func TestGetHeader_Bid_AcceptSSZ_ReturnsOctetStream(t *testing.T) {
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

	p := fakeBidProvider{bid: bid, err: nil}
	handler := httpapi.NewRouter(zap.NewNop(), p.Bid, nil, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	path := "/eth/v1/builder/header/1/0x" + hex32() + "/0x" + hex48()
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Accept", buildercodec.MediaTypeSSZ)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET header: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); got != buildercodec.MediaTypeSSZ {
		t.Fatalf("unexpected Content-Type: got %q want %q", got, buildercodec.MediaTypeSSZ)
	}

	want, err := bid.Deneb.MarshalSSZ()
	if err != nil {
		t.Fatalf("marshal ssz: %v", err)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("unexpected body bytes")
	}
}

func TestGetHeader_Bid_AcceptQValues_SelectsSSZ(t *testing.T) {
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

	p := fakeBidProvider{bid: bid, err: nil}
	handler := httpapi.NewRouter(zap.NewNop(), p.Bid, nil, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	path := "/eth/v1/builder/header/1/0x" + hex32() + "/0x" + hex48()
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Accept", "application/json;q=0.1, application/octet-stream;q=0.9")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET header: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); got != buildercodec.MediaTypeSSZ {
		t.Fatalf("unexpected Content-Type: got %q want %q", got, buildercodec.MediaTypeSSZ)
	}
}

func TestGetHeader_Bid_AcceptWildcard_DefaultsToJSON(t *testing.T) {
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

	p := fakeBidProvider{bid: bid, err: nil}
	handler := httpapi.NewRouter(zap.NewNop(), p.Bid, nil, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	path := "/eth/v1/builder/header/1/0x" + hex32() + "/0x" + hex48()
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Accept", "*/*")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET header: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); got != buildercodec.MediaTypeJSON {
		t.Fatalf("unexpected Content-Type: got %q want %q", got, buildercodec.MediaTypeJSON)
	}
}

func TestGetHeader_Bid_NotAcceptable_Returns406(t *testing.T) {
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

	p := fakeBidProvider{bid: bid, err: nil}
	handler := httpapi.NewRouter(zap.NewNop(), p.Bid, nil, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	path := "/eth/v1/builder/header/1/0x" + hex32() + "/0x" + hex48()
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Accept", "text/plain")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET header: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotAcceptable {
		t.Fatalf("unexpected status code: got %d, want %d", resp.StatusCode, http.StatusNotAcceptable)
	}
}

func hex32() string { return "0000000000000000000000000000000000000000000000000000000000000000" }
func hex48() string {
	return "000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"
}

type fakeUnblinder struct {
	resp *api.VersionedSignedProposal
	err  error
}

func (u fakeUnblinder) Unblind(_ context.Context, _ *api.VersionedSignedBlindedBeaconBlock) (*api.VersionedSignedProposal, error) {
	return u.resp, u.err
}

func TestPostBlindedBlocks_MissingConsensusHeader(t *testing.T) {
	t.Parallel()

	u := fakeUnblinder{}
	handler := httpapi.NewRouter(zap.NewNop(), nil, u.Unblind, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/eth/v1/builder/blinded_blocks", buildercodec.MediaTypeJSON, strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST blinded_blocks: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected status: got %d want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestPostBlindedBlocks_UnblinderNotConfigured_Returns503(t *testing.T) {
	t.Parallel()

	handler := httpapi.NewRouter(zap.NewNop(), nil, nil, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/eth/v1/builder/blinded_blocks", buildercodec.MediaTypeJSON, strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST blinded_blocks: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unexpected status: got %d want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestPostBlindedBlocks_SSZ_MissingConsensusHeader_Returns400(t *testing.T) {
	t.Parallel()

	u := fakeUnblinder{}
	handler := httpapi.NewRouter(zap.NewNop(), nil, u.Unblind, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/eth/v1/builder/blinded_blocks", buildercodec.MediaTypeSSZ, bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("POST blinded_blocks: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected status: got %d want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestPostBlindedBlocks_UnsupportedContentType_Returns415(t *testing.T) {
	t.Parallel()

	u := fakeUnblinder{}
	handler := httpapi.NewRouter(zap.NewNop(), nil, u.Unblind, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	body := validDenebSignedBlindedBeaconBlockJSON(t)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/eth/v1/builder/blinded_blocks", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set(httpapi.EthConsensusVersion, "deneb")
	req.Header.Set("Content-Type", "text/plain")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST blinded_blocks: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("unexpected status: got %d want %d", resp.StatusCode, http.StatusUnsupportedMediaType)
	}
}

func TestPostBlindedBlocksV2_MissingConsensusHeader(t *testing.T) {
	t.Parallel()

	u := fakeUnblinder{}
	handler := httpapi.NewRouter(zap.NewNop(), nil, u.Unblind, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	// Builder API spec:
	// https://raw.githubusercontent.com/ethereum/builder-specs/main/apis/builder/blinded_blocks_v2.yaml
	//
	// Eth-Consensus-Version header is required for SSZ-encoded requests.
	resp, err := http.Post(srv.URL+"/eth/v2/builder/blinded_blocks", buildercodec.MediaTypeSSZ, bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("POST blinded_blocks v2: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected status: got %d want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestPostBlindedBlocksV2_UnblinderNotConfigured_Returns503(t *testing.T) {
	t.Parallel()

	handler := httpapi.NewRouter(zap.NewNop(), nil, nil, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/eth/v2/builder/blinded_blocks", buildercodec.MediaTypeJSON, strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST blinded_blocks v2: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unexpected status: got %d want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestPostBlindedBlocks_NoUnblindedBlock_Returns500(t *testing.T) {
	t.Parallel()

	u := fakeUnblinder{resp: nil, err: nil}
	handler := httpapi.NewRouter(zap.NewNop(), nil, u.Unblind, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	body := validDenebSignedBlindedBeaconBlockJSON(t)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/eth/v1/builder/blinded_blocks", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set(httpapi.EthConsensusVersion, "deneb")
	req.Header.Set("Content-Type", buildercodec.MediaTypeJSON)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST blinded_blocks: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("unexpected status: got %d want %d", resp.StatusCode, http.StatusInternalServerError)
	}
}

func TestPostBlindedBlocksV2_UnsupportedContentType_Returns415(t *testing.T) {
	t.Parallel()

	u := fakeUnblinder{resp: nil, err: nil}
	handler := httpapi.NewRouter(zap.NewNop(), nil, u.Unblind, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	body := validDenebSignedBlindedBeaconBlockJSON(t)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/eth/v2/builder/blinded_blocks", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set(httpapi.EthConsensusVersion, "deneb")
	req.Header.Set("Content-Type", "text/plain")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST blinded_blocks v2: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("unexpected status: got %d want %d", resp.StatusCode, http.StatusUnsupportedMediaType)
	}
}

func TestPostBlindedBlocksV2_NoUnblindedBlock_Returns500(t *testing.T) {
	t.Parallel()

	u := fakeUnblinder{resp: nil, err: nil}
	handler := httpapi.NewRouter(zap.NewNop(), nil, u.Unblind, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	body := validDenebSignedBlindedBeaconBlockJSON(t)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/eth/v2/builder/blinded_blocks", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set(httpapi.EthConsensusVersion, "deneb")
	req.Header.Set("Content-Type", buildercodec.MediaTypeJSON)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST blinded_blocks v2: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("unexpected status: got %d want %d", resp.StatusCode, http.StatusInternalServerError)
	}
}

func TestPostBlindedBlocks_Deneb_Returns200WithEnvelope(t *testing.T) {
	t.Parallel()

	// Minimal deneb proposal with required payload pointer(s) for JSON marshaling.
	denebProposal := &api.VersionedSignedProposal{
		Version: consensusspec.DataVersionDeneb,
		Deneb: &apiv1deneb.SignedBlockContents{
			SignedBlock: &deneb.SignedBeaconBlock{
				Message: &deneb.BeaconBlock{
					Body: &deneb.BeaconBlockBody{
						ExecutionPayload: &deneb.ExecutionPayload{BaseFeePerGas: uint256.NewInt(0)},
					},
				},
			},
		},
	}

	u := fakeUnblinder{resp: denebProposal, err: nil}
	handler := httpapi.NewRouter(zap.NewNop(), nil, u.Unblind, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	body := validDenebSignedBlindedBeaconBlockJSON(t)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/eth/v1/builder/blinded_blocks", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set(httpapi.EthConsensusVersion, "deneb")
	req.Header.Set("Content-Type", buildercodec.MediaTypeJSON)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST blinded_blocks: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get(httpapi.EthConsensusVersion); got != "deneb" {
		t.Fatalf("unexpected %s: got %q want %q", httpapi.EthConsensusVersion, got, "deneb")
	}

	var decoded map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := decoded["version"]; !ok {
		t.Fatalf("missing top-level version")
	}
	if _, ok := decoded["data"]; !ok {
		t.Fatalf("missing top-level data")
	}
}

func TestPostBlindedBlocks_Deneb_NoConsensusHeader_Returns200(t *testing.T) {
	t.Parallel()

	// Minimal deneb proposal with required payload pointer(s) for JSON marshaling.
	denebProposal := &api.VersionedSignedProposal{
		Version: consensusspec.DataVersionDeneb,
		Deneb: &apiv1deneb.SignedBlockContents{
			SignedBlock: &deneb.SignedBeaconBlock{
				Message: &deneb.BeaconBlock{
					Body: &deneb.BeaconBlockBody{
						ExecutionPayload: &deneb.ExecutionPayload{BaseFeePerGas: uint256.NewInt(0)},
					},
				},
			},
		},
	}

	u := fakeUnblinder{resp: denebProposal, err: nil}
	handler := httpapi.NewRouter(zap.NewNop(), nil, u.Unblind, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	body := validDenebSignedBlindedBeaconBlockJSON(t)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/eth/v1/builder/blinded_blocks", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	// Intentionally omit Eth-Consensus-Version; the spec allows it for JSON requests.
	req.Header.Set("Content-Type", buildercodec.MediaTypeJSON)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST blinded_blocks: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestPostBlindedBlocks_Deneb_AcceptSSZ_ReturnsOctetStream(t *testing.T) {
	t.Parallel()

	// Minimal deneb proposal with required payload pointer(s) for SSZ marshaling.
	denebProposal := &api.VersionedSignedProposal{
		Version: consensusspec.DataVersionDeneb,
		Deneb: &apiv1deneb.SignedBlockContents{
			SignedBlock: &deneb.SignedBeaconBlock{
				Message: &deneb.BeaconBlock{
					Body: &deneb.BeaconBlockBody{
						ExecutionPayload: &deneb.ExecutionPayload{BaseFeePerGas: uint256.NewInt(0)},
					},
				},
			},
		},
	}

	u := fakeUnblinder{resp: denebProposal, err: nil}
	handler := httpapi.NewRouter(zap.NewNop(), nil, u.Unblind, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	body := validDenebSignedBlindedBeaconBlockJSON(t)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/eth/v1/builder/blinded_blocks", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set(httpapi.EthConsensusVersion, "deneb")
	req.Header.Set("Content-Type", buildercodec.MediaTypeJSON)
	req.Header.Set("Accept", buildercodec.MediaTypeSSZ)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST blinded_blocks: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); got != buildercodec.MediaTypeSSZ {
		t.Fatalf("unexpected Content-Type: got %q want %q", got, buildercodec.MediaTypeSSZ)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected non-empty SSZ response")
	}
}

func TestPostBlindedBlocks_Deneb_AcceptSSZ_ResponseBytesMatchCodec(t *testing.T) {
	t.Parallel()

	denebProposal := &api.VersionedSignedProposal{
		Version: consensusspec.DataVersionDeneb,
		Deneb: &apiv1deneb.SignedBlockContents{
			SignedBlock: &deneb.SignedBeaconBlock{
				Message: &deneb.BeaconBlock{
					Body: &deneb.BeaconBlockBody{
						ExecutionPayload:      &deneb.ExecutionPayload{BaseFeePerGas: uint256.NewInt(0)},
						BlobKZGCommitments:    []deneb.KZGCommitment{},
						BLSToExecutionChanges: nil,
					},
				},
			},
			KZGProofs: []deneb.KZGProof{},
			Blobs:     []deneb.Blob{},
		},
	}

	u := fakeUnblinder{resp: denebProposal, err: nil}
	handler := httpapi.NewRouter(zap.NewNop(), nil, u.Unblind, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	body := validDenebSignedBlindedBeaconBlockJSON(t)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/eth/v1/builder/blinded_blocks", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set(httpapi.EthConsensusVersion, "deneb")
	req.Header.Set("Content-Type", buildercodec.MediaTypeJSON)
	req.Header.Set("Accept", "application/octet-stream;q=1,application/json;q=0.9")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST blinded_blocks: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); got != buildercodec.MediaTypeSSZ {
		t.Fatalf("unexpected Content-Type: got %q want %q", got, buildercodec.MediaTypeSSZ)
	}

	want, err := buildercodec.MarshalSubmitBlindedBlockResponseSSZ(denebProposal)
	if err != nil {
		t.Fatalf("marshal expected ssz: %v", err)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("unexpected body bytes")
	}
}

func TestPostBlindedBlocks_AcceptNotAcceptable_Returns406(t *testing.T) {
	t.Parallel()

	denebProposal := &api.VersionedSignedProposal{
		Version: consensusspec.DataVersionDeneb,
		Deneb: &apiv1deneb.SignedBlockContents{
			SignedBlock: &deneb.SignedBeaconBlock{
				Message: &deneb.BeaconBlock{
					Body: &deneb.BeaconBlockBody{
						ExecutionPayload: &deneb.ExecutionPayload{BaseFeePerGas: uint256.NewInt(0)},
					},
				},
			},
		},
	}

	u := fakeUnblinder{resp: denebProposal, err: nil}
	handler := httpapi.NewRouter(zap.NewNop(), nil, u.Unblind, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	body := validDenebSignedBlindedBeaconBlockJSON(t)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/eth/v1/builder/blinded_blocks", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set(httpapi.EthConsensusVersion, "deneb")
	req.Header.Set("Content-Type", buildercodec.MediaTypeJSON)
	req.Header.Set("Accept", "text/plain")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST blinded_blocks: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotAcceptable {
		t.Fatalf("unexpected status: got %d want %d", resp.StatusCode, http.StatusNotAcceptable)
	}
}

func TestPostBlindedBlocksV2_Deneb_Returns202Empty(t *testing.T) {
	t.Parallel()

	// Minimal deneb proposal with required payload pointer(s) for JSON marshaling.
	denebProposal := &api.VersionedSignedProposal{
		Version: consensusspec.DataVersionDeneb,
		Deneb: &apiv1deneb.SignedBlockContents{
			SignedBlock: &deneb.SignedBeaconBlock{
				Message: &deneb.BeaconBlock{
					Body: &deneb.BeaconBlockBody{
						ExecutionPayload: &deneb.ExecutionPayload{BaseFeePerGas: uint256.NewInt(0)},
					},
				},
			},
		},
	}

	u := fakeUnblinder{resp: denebProposal, err: nil}
	handler := httpapi.NewRouter(zap.NewNop(), nil, u.Unblind, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	body := validDenebSignedBlindedBeaconBlockJSON(t)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/eth/v2/builder/blinded_blocks", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set(httpapi.EthConsensusVersion, "deneb")
	req.Header.Set("Content-Type", buildercodec.MediaTypeJSON)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST blinded_blocks v2: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("unexpected status: got %d want %d", resp.StatusCode, http.StatusAccepted)
	}

	// v2 endpoint has an empty response body.
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("expected empty body, got %d bytes", len(data))
	}
}

func validDenebSignedBlindedBeaconBlockJSON(t *testing.T) []byte {
	t.Helper()

	payload := map[string]any{
		"message": map[string]any{
			"slot":           "1",
			"proposer_index": "1",
			"parent_root":    "0x" + hex32(),
			"state_root":     "0x" + hex32(),
			"body": map[string]any{
				"randao_reveal": "0x" + hex96(),
				"eth1_data": map[string]any{
					"deposit_root":  "0x" + hex32(),
					"deposit_count": "0",
					"block_hash":    "0x" + hex32(),
				},
				"graffiti":           "0x" + hex32(),
				"proposer_slashings": []any{},
				"attester_slashings": []any{},
				"attestations":       []any{},
				"deposits":           []any{},
				"voluntary_exits":    []any{},
				"sync_aggregate": map[string]any{
					"sync_committee_bits":      "0x" + hex64(),
					"sync_committee_signature": "0x" + hex96(),
				},
				"execution_payload_header": map[string]any{
					"parent_hash":       "0x" + hex32(),
					"fee_recipient":     "0x" + hex20(),
					"state_root":        "0x" + hex32(),
					"receipts_root":     "0x" + hex32(),
					"logs_bloom":        "0x" + hex256(),
					"prev_randao":       "0x" + hex32(),
					"block_number":      "0",
					"gas_limit":         "0",
					"gas_used":          "0",
					"timestamp":         "0",
					"extra_data":        "0x",
					"base_fee_per_gas":  "0",
					"block_hash":        "0x" + hex32(),
					"transactions_root": "0x" + hex32(),
					"withdrawals_root":  "0x" + hex32(),
					"blob_gas_used":     "0",
					"excess_blob_gas":   "0",
				},
				"bls_to_execution_changes": []any{},
				"blob_kzg_commitments":     []any{},
			},
		},
		"signature": "0x" + hex96(),
	}

	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal blinded block json: %v", err)
	}
	return b
}

func hex20() string  { return strings.Repeat("0", 40) }
func hex64() string  { return strings.Repeat("0", 128) }
func hex96() string  { return strings.Repeat("0", 192) }
func hex256() string { return strings.Repeat("0", 512) }
