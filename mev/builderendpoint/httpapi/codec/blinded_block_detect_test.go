package codec_test

import (
	"encoding/json"
	"testing"

	"github.com/ssvlabs/ssv/mev/builderendpoint/httpapi/codec"
)

func TestDetectConsensusVersionFromSignedBlindedBeaconBlockJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body map[string]any
		want string
	}{
		{
			name: "Bellatrix",
			body: map[string]any{
				"execution_payload_header": map[string]any{
					"parent_hash": "0x" + zeros(64),
				},
			},
			want: "bellatrix",
		},
		{
			name: "Capella",
			body: map[string]any{
				"execution_payload_header": map[string]any{
					"parent_hash":      "0x" + zeros(64),
					"withdrawals_root": "0x" + zeros(64),
				},
			},
			want: "capella",
		},
		{
			name: "Deneb",
			body: map[string]any{
				"execution_payload_header": map[string]any{
					"parent_hash":     "0x" + zeros(64),
					"blob_gas_used":   "0",
					"excess_blob_gas": "0",
				},
			},
			want: "deneb",
		},
		{
			name: "Electra",
			body: map[string]any{
				"execution_requests": []any{},
				"execution_payload_header": map[string]any{
					"parent_hash": "0x" + zeros(64),
				},
			},
			want: "electra",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			payload := map[string]any{
				"message": map[string]any{
					"body": tt.body,
				},
				"signature": "0x" + zeros(192),
			}
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			got, err := codec.DetectConsensusVersionFromSignedBlindedBeaconBlockJSON(data)
			if err != nil {
				t.Fatalf("detect: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func zeros(n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = '0'
	}
	return string(out)
}
