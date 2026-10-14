package goclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/beacon/goclient/mocks"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// GoClient must satisfy the Gloas proposer beacon-node surface.
var _ beacon.GloasProposerCalls = (*GoClient)(nil)

// recordedRequest is one request a route recorded: its HTTP method, and for a failed one the response
// status (0 when the failure carries none).
type recordedRequest struct {
	method string
	failed bool
	status int
}

// requestLog collects what a route records through its requestRecorder.
type requestLog []recordedRequest

func (l *requestLog) record(httpMethod string, _ time.Duration, err error) {
	*l = append(*l, recordedRequest{method: httpMethod, failed: err != nil, status: responseStatusCode(err)})
}

// discardRequest is a requestRecorder for tests that don't assert on recording.
func discardRequest(string, time.Duration, error) {}

// GetGloasBeaconBlock moves on from a beacon node that is down, and a pre-#630 node behind it still serves
// the block through the GET fallback.
func TestGetGloasBeaconBlock_NextClientFallsBackToGET(t *testing.T) {
	blockSSZ, err := gloas.TestingBeaconBlock(7).MarshalSSZ()
	require.NoError(t, err)

	down := httptest.NewServer(http.NotFoundHandler())
	down.Close() // refuses connections: the first beacon node is down

	var mu sync.Mutex
	var methods []string
	srv := mocks.NewServerWithHandler(func(r *http.Request, resp mocks.Response) (mocks.Response, error) {
		if r.URL.Path != "/eth/v4/validator/blocks/7" {
			return resp, nil
		}
		mu.Lock()
		methods = append(methods, r.Method)
		mu.Unlock()
		if r.Method == http.MethodPost {
			return mocks.NewResponse(nil, mocks.WithStatusCode(http.StatusMethodNotAllowed)), nil // predates beacon-APIs#630
		}
		return mocks.NewResponse(blockSSZ, mocks.WithHeader("Content-Type", "application/octet-stream")), nil
	})
	defer srv.Close()

	client, err := New(t.Context(), zap.NewNop(), Options{BeaconNodeAddr: down.URL + ";" + srv.URL, CommonTimeout: 400 * time.Millisecond, LongTimeout: 500 * time.Millisecond})
	require.NoError(t, err)

	got, err := client.GetGloasBeaconBlock(t.Context(), 7, []byte{0x02}, []byte{0x01}, nil)
	require.NoError(t, err)
	require.Equal(t, phase0.Slot(7), got.Block.Slot)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{http.MethodPost, http.MethodGet}, methods, "the second node rejects the POST and serves the GET")
}

// With no builder config, produce still POSTs (produceBlockV4 is POST-first per beacon-APIs#630), carrying
// a neutral local-build body: empty builders with the neutral boost factor (100).
func TestRequestGloasBeaconBlock(t *testing.T) {
	blockSSZ, err := gloas.TestingBeaconBlock(7).MarshalSSZ()
	require.NoError(t, err)

	var gotMethod, gotPath, gotRandao, gotGraffiti, gotAccept, gotContentType, gotIncludePayload string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotRandao = r.URL.Query().Get("randao_reveal")
		gotGraffiti = r.URL.Query().Get("graffiti")
		gotIncludePayload = r.URL.Query().Get("include_payload")
		gotAccept = r.Header.Get("Accept")
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write(blockSSZ)
	}))
	defer srv.Close()

	var requests requestLog
	got, err := requestGloasBeaconBlock(context.Background(), srv.URL, 7, []byte{0x02}, []byte{0x01}, nil, requests.record)
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, requestLog{{method: http.MethodPost}}, requests)
	require.Equal(t, "/eth/v4/validator/blocks/7", gotPath)
	require.Equal(t, "true", gotIncludePayload) // a self-build answers with BlockContents (SIP #94 §6)
	require.Equal(t, "0x01", gotRandao)         // randao is the 5th arg, graffiti the 4th
	// graffiti is padded to a full 32-byte value before hex-encoding (lighthouse rejects a short one).
	require.Equal(t, "0x02"+strings.Repeat("00", 31), gotGraffiti)
	require.Equal(t, "application/octet-stream", gotAccept)
	require.Equal(t, "application/json", gotContentType)
	// the neutral local-build body: no builders, p2p bids weighed at par with the local build (100).
	require.Contains(t, string(gotBody), `"builders":[]`)
	require.Contains(t, string(gotBody), `"builder_boost_factor":"100"`)
	require.Equal(t, phase0.Slot(7), got.Block.Slot)
	require.Nil(t, got.Envelope, "a bare block response carries no reveal data")
}

// A self-build response is BlockContents, flagged by Eth-Execution-Payload-Included: the block plus the
// envelope, blobs, and KZG proofs the operator reveals in §6.
func TestRequestGloasBeaconBlock_SelfBuildContents(t *testing.T) {
	contents := &gloas.BlockContents{
		Block:                    gloas.TestingBeaconBlock(7),
		ExecutionPayloadEnvelope: minimalExecutionPayloadEnvelope(),
		KZGProofs:                []deneb.KZGProof{{0x01}, {0x02}},
		Blobs:                    []deneb.Blob{{0x03}},
	}
	contentsSSZ, err := contents.MarshalSSZ()
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Eth-Execution-Payload-Included", "true")
		_, _ = w.Write(contentsSSZ)
	}))
	defer srv.Close()

	got, err := requestGloasBeaconBlock(context.Background(), srv.URL, 7, []byte{0x02}, []byte{0x01}, nil, discardRequest)
	require.NoError(t, err)
	require.Equal(t, phase0.Slot(7), got.Block.Slot)
	require.Empty(t, got.BuilderURL)
	require.NotNil(t, got.Envelope)
	require.Equal(t, gloas.BuilderIndexSelfBuild, got.Envelope.Envelope.BuilderIndex)
	require.Equal(t, contents.KZGProofs, got.Envelope.KZGProofs)
	require.Equal(t, contents.Blobs, got.Envelope.Blobs)
}

// A BlockContents body not flagged by Eth-Execution-Payload-Included is a beacon-node contract violation;
// produce fails naming the header rather than misreading the body as a block.
func TestRequestGloasBeaconBlock_ContentsWithoutHeaderFails(t *testing.T) {
	contents := &gloas.BlockContents{
		Block:                    gloas.TestingBeaconBlock(7),
		ExecutionPayloadEnvelope: minimalExecutionPayloadEnvelope(),
	}
	contentsSSZ, err := contents.MarshalSSZ()
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(contentsSSZ)
	}))
	defer srv.Close()

	_, err = requestGloasBeaconBlock(context.Background(), srv.URL, 7, []byte{0x02}, []byte{0x01}, nil, discardRequest)
	require.ErrorContains(t, err, "Eth-Execution-Payload-Included")
}

// An unconfigured cluster against a beacon node that predates the produceBlockV4 POST (beacon-APIs#630):
// the neutral POST is rejected (405) and the fallback GET carries the neutral boost factor (100).
func TestRequestGloasBeaconBlock_UnconfiguredFallbackToGET(t *testing.T) {
	blockSSZ, err := gloas.TestingBeaconBlock(7).MarshalSSZ()
	require.NoError(t, err)

	var methods []string
	var getBoost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed) // node predates beacon-APIs#630 (GET-only)
			return
		}
		getBoost = r.URL.Query().Get("builder_boost_factor")
		_, _ = w.Write(blockSSZ)
	}))
	defer srv.Close()

	var requests requestLog
	got, err := requestGloasBeaconBlock(context.Background(), srv.URL, 7, []byte{0x02}, []byte{0x01}, nil, requests.record)
	require.NoError(t, err)
	require.Equal(t, []string{http.MethodPost, http.MethodGet}, methods, "unconfigured POST 405 falls back to GET")
	require.Equal(t, requestLog{{method: http.MethodPost, failed: true, status: http.StatusMethodNotAllowed}, {method: http.MethodGet}}, requests,
		"the rejected POST and the fallback GET are recorded as two requests")
	require.Equal(t, "100", getBoost, "the fallback GET carries the neutral builder_boost_factor")
	require.Equal(t, phase0.Slot(7), got.Block.Slot)
}

// With a builder config, produce is a POST carrying the JSON BuilderConfig body and the winning builder's
// Eth-Builder-Url is read back from the response (beacon-APIs#630).
func TestRequestGloasBeaconBlock_POST(t *testing.T) {
	blockSSZ, err := gloas.TestingBeaconBlock(7).MarshalSSZ()
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
	var requests requestLog
	got, err := requestGloasBeaconBlock(context.Background(), srv.URL, 7, []byte{0x02}, []byte{0x01}, cfg, requests.record)
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, requestLog{{method: http.MethodPost}}, requests)
	require.Equal(t, "application/json", gotContentType)
	require.Equal(t, "gloas", gotConsensusVersion)
	require.Contains(t, string(gotBody), `"min_bid":"10"`)
	require.Equal(t, "https://builder.example.com", got.BuilderURL)
	require.Equal(t, phase0.Slot(7), got.Block.Slot)
}

// A beacon node that predates the produceBlockV4 POST answers it with 404; produce then retries that node
// as the legacy GET, carrying builder_boost_factor (the one knob the pre-#630 GET also honors).
func TestRequestGloasBeaconBlock_POSTFallbackToGET(t *testing.T) {
	blockSSZ, err := gloas.TestingBeaconBlock(7).MarshalSSZ()
	require.NoError(t, err)

	var methods []string
	var getBoost, getAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusNotFound) // node predates beacon-APIs#630
			return
		}
		getBoost = r.URL.Query().Get("builder_boost_factor")
		getAccept = r.Header.Get("Accept")
		_, _ = w.Write(blockSSZ)
	}))
	defer srv.Close()

	cfg := &gloas.ProduceBuilderConfig{BuilderBoostFactor: 150}
	var requests requestLog
	got, err := requestGloasBeaconBlock(context.Background(), srv.URL, 7, []byte{0x02}, []byte{0x01}, cfg, requests.record)
	require.NoError(t, err)
	require.Equal(t, []string{http.MethodPost, http.MethodGet}, methods, "POST 404 falls back to GET")
	require.Equal(t, requestLog{{method: http.MethodPost, failed: true, status: http.StatusNotFound}, {method: http.MethodGet}}, requests,
		"the rejected POST and the fallback GET are recorded as two requests")
	require.Equal(t, "150", getBoost, "the fallback GET carries the configured builder_boost_factor")
	require.Equal(t, "application/octet-stream", getAccept, "the fallback GET still asks for the SSZ block")
	require.Equal(t, phase0.Slot(7), got.Block.Slot)
	require.Empty(t, got.BuilderURL)
}

// A produce response tagged with a non-Gloas Eth-Consensus-Version is rejected — a wrong-fork guard.
func TestRequestGloasBeaconBlock_WrongConsensusVersion(t *testing.T) {
	blockSSZ, err := gloas.TestingBeaconBlock(7).MarshalSSZ()
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Eth-Consensus-Version", "fulu")
		_, _ = w.Write(blockSSZ)
	}))
	defer srv.Close()

	var requests requestLog
	_, err = requestGloasBeaconBlock(context.Background(), srv.URL, 7, []byte{0x02}, []byte{0x01}, nil, requests.record)
	require.ErrorContains(t, err, "Eth-Consensus-Version")
	require.ErrorContains(t, err, "fulu")
	require.Equal(t, requestLog{{method: http.MethodPost, failed: true}}, requests,
		"a wrong-fork response is recorded as a failed POST and does not fall back to GET")
}

// Only a missing route or method (404/405) falls back: a POST the beacon node fails is returned as is.
func TestRequestGloasBeaconBlock_POSTServerErrorDoesNotFallBack(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	var requests requestLog
	_, err := requestGloasBeaconBlock(context.Background(), srv.URL, 7, []byte{0x02}, []byte{0x01}, nil, requests.record)
	require.Equal(t, http.StatusInternalServerError, responseStatusCode(err))
	require.Equal(t, []string{http.MethodPost}, methods, "a 500 is no reason to retry as GET")
	require.Equal(t, requestLog{{method: http.MethodPost, failed: true, status: http.StatusInternalServerError}}, requests)
}

// A fallback GET that fails too is recorded as a second, failed request, and its error is returned.
func TestRequestGloasBeaconBlock_FallbackGETFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusNotFound) // node predates beacon-APIs#630
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	var requests requestLog
	_, err := requestGloasBeaconBlock(context.Background(), srv.URL, 7, []byte{0x02}, []byte{0x01}, nil, requests.record)
	require.Equal(t, http.StatusServiceUnavailable, responseStatusCode(err), "the GET's failure is the one returned")
	require.Equal(t, requestLog{
		{method: http.MethodPost, failed: true, status: http.StatusNotFound},
		{method: http.MethodGet, failed: true, status: http.StatusServiceUnavailable},
	}, requests)
}

func TestSubmitGloasBeaconBlock(t *testing.T) {
	var gotMethod, gotPath, gotVersion, gotAccept, gotContentType string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotVersion = r.Header.Get("Eth-Consensus-Version")
		gotAccept = r.Header.Get("Accept")
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
	// the route answers with no content and JSON errors; Prysm refuses an SSZ-only Accept with 406.
	require.Equal(t, "application/json", gotAccept)
	require.Equal(t, "application/octet-stream", gotContentType)
	require.Equal(t, []byte{0x01, 0x02}, gotBody)
}

// The Eth-Builder-Url echo (owner-match forwarding, beacon-APIs#630) must reach the publish POST as a
// request header so the beacon node forwards the block to the winning builder.
func TestSubmitGloasBeaconBlock_EchoesBuilderURL(t *testing.T) {
	var gotBuilderURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBuilderURL = r.Header.Get("Eth-Builder-Url")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := submitGloasBeaconBlock(context.Background(), srv.URL, []byte{0x01, 0x02},
		map[string]string{"Eth-Builder-Url": "https://builder.example.com"})
	require.NoError(t, err)
	require.Equal(t, "https://builder.example.com", gotBuilderURL)
}

// A block the beacon node already knows is treated as a successful submit: every operator submits the
// decided block for redundancy, so non-leader duplicates must not surface as errors.
func TestSubmitGloasBeaconBlock_AlreadyKnownIsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"code":500,"message":"BLOCK_ERROR_ALREADY_KNOWN"}`) // Lodestar's response before v1.47
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
