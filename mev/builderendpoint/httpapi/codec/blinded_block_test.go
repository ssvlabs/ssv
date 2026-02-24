package codec_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/attestantio/go-eth2-client/spec"

	"github.com/ssvlabs/ssv/mev/builderendpoint/httpapi/codec"
)

func TestUnmarshalBlindedBlockJSON_Deneb(t *testing.T) {
	t.Parallel()

	body := validDenebSignedBlindedBeaconBlockJSON(t)

	got, err := codec.UnmarshalBlindedBlock("application/json", "deneb", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got == nil {
		t.Fatalf("expected block")
	}
	if got.Version != spec.DataVersionDeneb {
		t.Fatalf("unexpected version: got %v want %v", got.Version, spec.DataVersionDeneb)
	}
	if got.Deneb == nil {
		t.Fatalf("expected deneb body")
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
					"sync_committee_bits":      "0x" + strings.Repeat("0", 128),
					"sync_committee_signature": "0x" + hex96(),
				},
				"execution_payload_header": map[string]any{
					"parent_hash":       "0x" + hex32(),
					"fee_recipient":     "0x" + strings.Repeat("0", 40),
					"state_root":        "0x" + hex32(),
					"receipts_root":     "0x" + hex32(),
					"logs_bloom":        "0x" + strings.Repeat("0", 512),
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

func hex32() string { return strings.Repeat("0", 64) }
func hex96() string { return strings.Repeat("0", 192) }
