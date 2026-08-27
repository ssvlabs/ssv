package goclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// GoClient must satisfy the Gloas §6 envelope beacon-node surface.
var _ beacon.GloasEnvelopeCalls = (*GoClient)(nil)

func minimalExecutionPayloadEnvelope() *gloas.ExecutionPayloadEnvelope {
	return &gloas.ExecutionPayloadEnvelope{
		Payload:           &gloas.ExecutionPayload{},
		ExecutionRequests: &gloas.ExecutionRequests{},
		BuilderIndex:      gloas.BuilderIndexSelfBuild,
	}
}

func TestRequestExecutionPayloadEnvelope(t *testing.T) {
	envelopeSSZ, err := minimalExecutionPayloadEnvelope().MarshalSSZ()
	require.NoError(t, err)

	var gotMethod, gotPath, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotAccept = r.Header.Get("Accept")
		_, _ = w.Write(envelopeSSZ)
	}))
	defer srv.Close()

	got, err := requestExecutionPayloadEnvelope(context.Background(), srv.URL, 9, phase0.Root{0xab})
	require.NoError(t, err)
	require.Equal(t, http.MethodGet, gotMethod)
	// plural collection; beacon_block_root is a path segment, not a query param.
	require.Equal(t, "/eth/v1/validator/execution_payload_envelopes/9/0xab"+strings.Repeat("0", 62), gotPath)
	require.Equal(t, "application/octet-stream", gotAccept)
	require.Equal(t, gloas.BuilderIndexSelfBuild, got.BuilderIndex)
}

func TestSubmitExecutionPayloadEnvelope(t *testing.T) {
	var gotMethod, gotPath, gotVersion, gotContentType, gotBlobDataIncluded string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotVersion = r.Header.Get("Eth-Consensus-Version")
		gotContentType = r.Header.Get("Content-Type")
		gotBlobDataIncluded = r.Header.Get("Eth-Blob-Data-Included")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := submitExecutionPayloadEnvelope(context.Background(), srv.URL, []byte{0x01, 0x02})
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "/eth/v1/beacon/execution_payload_envelopes", gotPath)
	require.Equal(t, consensusVersionGloas, gotVersion)
	// full envelope (stateful flow), not the blobs-carrying Contents — the required beacon-APIs#624 header.
	require.Equal(t, "false", gotBlobDataIncluded)
	require.Equal(t, "application/octet-stream", gotContentType)
	require.Equal(t, []byte{0x01, 0x02}, gotBody)
}

// The publish sends the full SignedExecutionPayloadEnvelope SSZ (what Lodestar v1.43.0 decodes), not the
// blinded form the node signs over.
func TestSubmitExecutionPayloadEnvelope_PublishesFullSignedEnvelope(t *testing.T) {
	signed := &gloas.SignedExecutionPayloadEnvelope{
		Message:   minimalExecutionPayloadEnvelope(),
		Signature: phase0.BLSSignature{0x01},
	}
	wantBody, err := signed.MarshalSSZ()
	require.NoError(t, err)

	var gotBlobDataIncluded string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBlobDataIncluded = r.Header.Get("Eth-Blob-Data-Included")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &aggregatorClientMock{}
	gc := &GoClient{
		log:             zap.NewNop(),
		clients:         []Client{client},
		clientAddresses: map[Client]string{client: srv.URL},
		commonTimeout:   time.Second,
	}

	require.NoError(t, gc.SubmitExecutionPayloadEnvelope(t.Context(), signed))
	require.Equal(t, "false", gotBlobDataIncluded, "publish selects the full envelope, not the blobs-carrying Contents")
	require.Equal(t, wantBody, gotBody, "publish must send the full signed envelope SSZ")
}

// An envelope the beacon node already knows is treated as a successful publish: on self-build every
// operator publishes the identical envelope, so the non-winning ones race the canonical one (§6 analog
// of the §4 block submit).
func TestSubmitExecutionPayloadEnvelope_AlreadyKnownIsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"code":500,"message":"EXECUTION_PAYLOAD_ENVELOPE_ERROR_ALREADY_KNOWN"}`) // Lodestar's response
	}))
	defer srv.Close()

	require.NoError(t, submitExecutionPayloadEnvelope(context.Background(), srv.URL, []byte{0x01, 0x02}))
}
