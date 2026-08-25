package goclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	bitfield "github.com/prysmaticlabs/go-bitfield"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// GoClient must satisfy the Gloas proposer beacon-node surface.
var _ beacon.GloasProposerCalls = (*GoClient)(nil)

func minimalGloasBlock() *gloas.BeaconBlock {
	return &gloas.BeaconBlock{
		Slot: 7,
		Body: &gloas.BeaconBlockBody{
			ETH1Data:                  &phase0.ETH1Data{BlockHash: make([]byte, 32)},
			SyncAggregate:             &altair.SyncAggregate{SyncCommitteeBits: bitfield.NewBitvector512()},
			SignedExecutionPayloadBid: &gloas.SignedExecutionPayloadBid{Message: &gloas.ExecutionPayloadBid{BuilderIndex: gloas.BuilderIndexSelfBuild}},
			ParentExecutionRequests:   &gloas.ExecutionRequests{},
		},
	}
}

func TestRequestGloasBeaconBlock(t *testing.T) {
	blockSSZ, err := minimalGloasBlock().MarshalSSZ()
	require.NoError(t, err)

	var gotMethod, gotPath, gotRandao, gotGraffiti, gotAccept, gotIncludePayload string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotRandao = r.URL.Query().Get("randao_reveal")
		gotGraffiti = r.URL.Query().Get("graffiti")
		gotIncludePayload = r.URL.Query().Get("include_payload")
		gotAccept = r.Header.Get("Accept")
		_, _ = w.Write(blockSSZ)
	}))
	defer srv.Close()

	got, err := requestGloasBeaconBlock(context.Background(), srv.URL, 7, []byte{0x02}, []byte{0x01}, nil)
	require.NoError(t, err)
	require.Equal(t, http.MethodGet, gotMethod)
	require.Equal(t, "/eth/v4/validator/blocks/7", gotPath)
	require.Equal(t, "false", gotIncludePayload) // bare block; payload ships in the §6 envelope
	require.Equal(t, "0x01", gotRandao)          // randao is the 5th arg, graffiti the 4th
	// graffiti is padded to a full 32-byte value before hex-encoding (lighthouse rejects a short one).
	require.Equal(t, "0x02"+strings.Repeat("00", 31), gotGraffiti)
	require.Equal(t, "application/octet-stream", gotAccept)
	require.Equal(t, phase0.Slot(7), got.block.Slot)
}

// With a builder config, produce is a POST carrying the JSON BuilderConfig body and the winning builder's
// Eth-Builder-Url is read back from the response (beacon-APIs#630).
func TestRequestGloasBeaconBlock_POST(t *testing.T) {
	blockSSZ, err := minimalGloasBlock().MarshalSSZ()
	require.NoError(t, err)

	var gotMethod, gotContentType, gotConsensusVersion string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		gotConsensusVersion = r.Header.Get("Eth-Consensus-Version")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Eth-Builder-Url", "https://builder.example.com")
		_, _ = w.Write(blockSSZ)
	}))
	defer srv.Close()

	cfg := &gloas.ProduceBuilderConfig{
		MinBid:             10,
		BuilderBoostFactor: 100,
		Builders: []gloas.ProduceBuilderEntry{{
			URL:  "https://builder.example.com",
			Auth: &gloas.SignedBuilderRequestAuth{Message: &gloas.BuilderRequestAuth{Data: []byte{0x01}, Slot: 7}},
		}},
	}
	got, err := requestGloasBeaconBlock(context.Background(), srv.URL, 7, []byte{0x02}, []byte{0x01}, cfg)
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "application/json", gotContentType)
	require.Equal(t, "gloas", gotConsensusVersion)
	require.Contains(t, string(gotBody), `"min_bid":"10"`)
	require.Equal(t, "https://builder.example.com", got.builderURL)
	require.Equal(t, phase0.Slot(7), got.block.Slot)
}

// A beacon node that predates the produceBlockV4 POST answers it with 404; produce then retries that node
// as the legacy GET, transparently.
func TestRequestGloasBeaconBlock_POSTFallbackToGET(t *testing.T) {
	blockSSZ, err := minimalGloasBlock().MarshalSSZ()
	require.NoError(t, err)

	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusNotFound) // node predates beacon-APIs#630
			return
		}
		_, _ = w.Write(blockSSZ)
	}))
	defer srv.Close()

	cfg := &gloas.ProduceBuilderConfig{BuilderBoostFactor: 100}
	got, err := requestGloasBeaconBlock(context.Background(), srv.URL, 7, []byte{0x02}, []byte{0x01}, cfg)
	require.NoError(t, err)
	require.Equal(t, []string{http.MethodPost, http.MethodGet}, methods, "POST 404 falls back to GET")
	require.Equal(t, phase0.Slot(7), got.block.Slot)
	require.Empty(t, got.builderURL)
}

func TestSubmitGloasBeaconBlock(t *testing.T) {
	var gotMethod, gotPath, gotVersion, gotContentType string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotVersion = r.Header.Get("Eth-Consensus-Version")
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := submitGloasBeaconBlock(context.Background(), srv.URL, []byte{0x01, 0x02}, nil)
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "/eth/v2/beacon/blocks", gotPath)
	require.Equal(t, consensusVersionGloas, gotVersion)
	require.Equal(t, "application/octet-stream", gotContentType)
	require.Equal(t, []byte{0x01, 0x02}, gotBody)
}

func TestGloasOctetStreamHTTP_Non2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad block"))
	}))
	defer srv.Close()

	_, err := gloasOctetStreamHTTP(context.Background(), http.MethodGet, srv.URL, nil, nil)
	require.ErrorContains(t, err, "status 400")
}

// A block the beacon node already knows (canonical) is treated as a successful submit: every operator
// submits the decided block for redundancy, so non-leader duplicates must not surface as errors.
func TestSubmitGloasBeaconBlock_AlreadyKnownIsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"code":500,"message":"BLOCK_ERROR_ALREADY_KNOWN"}`) // Lodestar's response
	}))
	defer srv.Close()

	require.NoError(t, submitGloasBeaconBlock(context.Background(), srv.URL, []byte{0x01, 0x02}, nil))
}

// A genuine rejection (not "already known") still propagates as an error.
func TestSubmitGloasBeaconBlock_RealErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"code":400,"message":"invalid block"}`)
	}))
	defer srv.Close()

	require.Error(t, submitGloasBeaconBlock(context.Background(), srv.URL, []byte{0x01, 0x02}, nil))
}

func TestIsAlreadyKnown(t *testing.T) {
	require.False(t, isAlreadyKnown(nil))
	require.False(t, isAlreadyKnown(errors.New("some other error")))
	require.False(t, isAlreadyKnown(&gloasHTTPError{statusCode: http.StatusBadRequest, body: "invalid block"}))
	require.True(t, isAlreadyKnown(&gloasHTTPError{statusCode: http.StatusInternalServerError, body: `{"message":"BLOCK_ERROR_ALREADY_KNOWN"}`}))
	require.True(t, isAlreadyKnown(&gloasHTTPError{statusCode: http.StatusInternalServerError, body: `{"message":"EXECUTION_PAYLOAD_ENVELOPE_ERROR_ALREADY_KNOWN"}`}))
	require.True(t, isAlreadyKnown(&gloasHTTPError{statusCode: http.StatusAccepted, body: "block already known"}))
}
