package goclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/deneb"
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
	// the blobs-carrying Contents form — the required beacon-APIs#624 header (SIP #94 §6).
	require.Equal(t, "true", gotBlobDataIncluded)
	require.Equal(t, "application/octet-stream", gotContentType)
	require.Equal(t, []byte{0x01, 0x02}, gotBody)
}

// The publish body is SignedExecutionPayloadEnvelopeContents: the signed full envelope (the node signs over
// its blinded form, whose root is the same) with the blobs and KZG proofs any beacon node needs to broadcast it.
func TestSubmitExecutionPayloadEnvelope_PublishesContents(t *testing.T) {
	produced := &gloas.ProducedEnvelope{
		Envelope:  minimalExecutionPayloadEnvelope(),
		KZGProofs: []deneb.KZGProof{{0x01}},
		Blobs:     []deneb.Blob{{0x02}},
	}
	contents := produced.Signed(phase0.BLSSignature{0x01})
	wantBody, err := contents.MarshalSSZ()
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

	require.NoError(t, gc.SubmitExecutionPayloadEnvelope(t.Context(), contents))
	require.Equal(t, "true", gotBlobDataIncluded, "publish carries the blob data so any beacon node can broadcast it")
	require.Equal(t, wantBody, gotBody, "publish must send the contents SSZ")

	decoded := &gloas.SignedExecutionPayloadEnvelopeContents{}
	require.NoError(t, decoded.UnmarshalSSZ(gotBody))
	require.Equal(t, produced.Envelope.BuilderIndex, decoded.SignedExecutionPayloadEnvelope.Message.BuilderIndex)
	require.Equal(t, phase0.BLSSignature{0x01}, decoded.SignedExecutionPayloadEnvelope.Signature)
	require.Equal(t, produced.KZGProofs, decoded.KZGProofs)
	require.Equal(t, produced.Blobs, decoded.Blobs)
}

// An envelope the beacon node already knows is treated as a successful publish: the builder operator
// publishes to each of its beacon nodes, and operators sharing a beacon node publish the identical reveal
// (§6 analog of the §4 block submit).
func TestSubmitExecutionPayloadEnvelope_AlreadyKnownIsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"code":500,"message":"EXECUTION_PAYLOAD_ENVELOPE_ERROR_ALREADY_KNOWN"}`) // Lodestar's response
	}))
	defer srv.Close()

	require.NoError(t, submitExecutionPayloadEnvelope(context.Background(), srv.URL, []byte{0x01, 0x02}))
}
