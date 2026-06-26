package goclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// GoClient must satisfy the Gloas §6 envelope beacon-node surface.
var _ beacon.GloasEnvelopeCalls = (*GoClient)(nil)

func minimalExecutionPayloadEnvelope() *gloas.ExecutionPayloadEnvelope {
	return &gloas.ExecutionPayloadEnvelope{
		Payload:           &gloas.ExecutionPayload{},
		ExecutionRequests: &electra.ExecutionRequests{},
		BuilderIndex:      gloas.BuilderIndexSelfBuild,
	}
}

func TestRequestExecutionPayloadEnvelope(t *testing.T) {
	envelopeSSZ, err := minimalExecutionPayloadEnvelope().MarshalSSZ()
	require.NoError(t, err)

	var gotMethod, gotPath, gotRoot, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotRoot = r.URL.Query().Get("beacon_block_root")
		gotAccept = r.Header.Get("Accept")
		_, _ = w.Write(envelopeSSZ)
	}))
	defer srv.Close()

	got, err := requestExecutionPayloadEnvelope(context.Background(), srv.URL, 9, phase0.Root{0xab})
	require.NoError(t, err)
	require.Equal(t, http.MethodGet, gotMethod)
	require.Equal(t, "/eth/v1/validator/execution_payload_envelope/9", gotPath)
	require.Equal(t, "0xab"+strings.Repeat("0", 62), gotRoot) // 32-byte root, 0x-hex
	require.Equal(t, "application/octet-stream", gotAccept)
	require.Equal(t, gloas.BuilderIndexSelfBuild, got.BuilderIndex)
}

func TestSubmitExecutionPayloadEnvelope(t *testing.T) {
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

	err := submitExecutionPayloadEnvelope(context.Background(), srv.URL, []byte{0x01, 0x02})
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "/eth/v1/beacon/execution_payload_envelope", gotPath)
	require.Equal(t, consensusVersionGloas, gotVersion)
	require.Equal(t, "application/octet-stream", gotContentType)
	require.Equal(t, []byte{0x01, 0x02}, gotBody)
}
