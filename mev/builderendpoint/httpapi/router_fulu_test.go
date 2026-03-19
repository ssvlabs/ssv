package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/attestantio/go-eth2-client/api"
	apiv1fulu "github.com/attestantio/go-eth2-client/api/v1/fulu"
	consensusspec "github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/holiman/uint256"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/mev/builderendpoint/httpapi"
	buildercodec "github.com/ssvlabs/ssv/mev/builderendpoint/httpapi/codec"
)

func TestPostBlindedBlocks_Fulu_NoConsensusHeader_ResponseHeaderMatchesProposalVersion(t *testing.T) {
	t.Parallel()

	fuluProposal := &api.VersionedSignedProposal{
		Version: consensusspec.DataVersionFulu,
		Fulu: &apiv1fulu.SignedBlockContents{
			SignedBlock: &electra.SignedBeaconBlock{
				Message: &electra.BeaconBlock{
					Body: &electra.BeaconBlockBody{
						ExecutionPayload:   &deneb.ExecutionPayload{BaseFeePerGas: uint256.NewInt(0)},
						BlobKZGCommitments: []deneb.KZGCommitment{},
					},
				},
			},
			KZGProofs: []deneb.KZGProof{},
			Blobs:     []deneb.Blob{},
		},
	}

	u := fakeUnblinder{resp: fuluProposal, err: nil}
	handler := httpapi.NewRouter(zap.NewNop(), nil, u.Unblind, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/eth/v1/builder/blinded_blocks", bytes.NewReader(validElectraLikeSignedBlindedBeaconBlockJSON(t)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", buildercodec.MediaTypeJSON)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST blinded_blocks: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get(httpapi.EthConsensusVersion); got != "fulu" {
		t.Fatalf("unexpected %s: got %q want %q", httpapi.EthConsensusVersion, got, "fulu")
	}
}

func validElectraLikeSignedBlindedBeaconBlockJSON(t *testing.T) []byte {
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
				"execution_requests": map[string]any{
					"deposits":       []any{},
					"withdrawals":    []any{},
					"consolidations": []any{},
				},
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
