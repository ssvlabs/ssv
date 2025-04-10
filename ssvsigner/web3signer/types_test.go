package web3signer

import (
	"encoding/json"
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
)

func TestDataVersion_MarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    DataVersion
		expected string
	}{
		{"Phase0", DataVersion(spec.DataVersionPhase0), `"PHASE0"`},
		{"Altair", DataVersion(spec.DataVersionAltair), `"ALTAIR"`},
		{"Bellatrix", DataVersion(spec.DataVersionBellatrix), `"BELLATRIX"`},
		{"Capella", DataVersion(spec.DataVersionCapella), `"CAPELLA"`},
		{"Deneb", DataVersion(spec.DataVersionDeneb), `"DENEB"`},
		{"Electra", DataVersion(spec.DataVersionElectra), `"ELECTRA"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			output, err := json.Marshal(&tt.input)

			require.NoError(t, err)
			require.Equal(t, tt.expected, string(output))
		})
	}
}

func TestDataVersion_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected DataVersion
	}{
		{"Phase0 upper", `"PHASE0"`, DataVersion(spec.DataVersionPhase0)},
		{"Altair lower", `"altair"`, DataVersion(spec.DataVersionAltair)},
		{"Mixed case", `"BeLLaTrIx"`, DataVersion(spec.DataVersionBellatrix)},
		{"Deneb upper", `"DENEB"`, DataVersion(spec.DataVersionDeneb)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var dv DataVersion
			err := json.Unmarshal([]byte(tt.input), &dv)

			require.NoError(t, err)
			require.Equal(t, tt.expected, dv)
		})
	}
}

func TestDataVersion_RoundTrip(t *testing.T) {
	t.Parallel()

	original := DataVersion(spec.DataVersionCapella)

	// Marshal
	data, err := json.Marshal(&original)

	require.NoError(t, err)
	require.Equal(t, `"CAPELLA"`, string(data))

	// Unmarshal back
	var decoded DataVersion
	err = json.Unmarshal(data, &decoded)

	require.NoError(t, err)
	require.Equal(t, original, decoded)
}

// TestAggregateAndProof_MarshalJSON tests the MarshalJSON method of AggregateAndProof
func TestAggregateAndProof_MarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       *AggregateAndProof
		expectError bool
	}{
		{
			name:        "Nil AggregateAndProof",
			input:       nil,
			expectError: false,
		},
		{
			name:        "Empty AggregateAndProof",
			input:       &AggregateAndProof{},
			expectError: false,
		},
		{
			name: "Phase0 AggregateAndProof",
			input: &AggregateAndProof{
				Phase0: &phase0.AggregateAndProof{
					AggregatorIndex: 123,
					SelectionProof:  phase0.BLSSignature{0x01},
				},
			},
			expectError: false,
		},
		{
			name: "Electra AggregateAndProof",
			input: &AggregateAndProof{
				Electra: &electra.AggregateAndProof{
					AggregatorIndex: 456,
					SelectionProof:  phase0.BLSSignature{0x02},
				},
			},
			expectError: false,
		},
		{
			name: "Both Phase0 and Electra set (error case)",
			input: &AggregateAndProof{
				Phase0: &phase0.AggregateAndProof{
					AggregatorIndex: 123,
				},
				Electra: &electra.AggregateAndProof{
					AggregatorIndex: 456,
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := json.Marshal(tt.input)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), "both phase0 and electra cannot be set")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestAggregateAndProof_UnmarshalJSON tests unmarshaling JSON to either Phase0 or Electra type.
func TestAggregateAndProof_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	// Test case 1: JSON with "committee_bits" gets unmarshaled to Electra
	t.Run("Unmarshal JSON with committee_bits to Electra", func(t *testing.T) {
		t.Parallel()

		jsonData := []byte(`{
			"aggregator_index": "123",
			"selection_proof": "0xb4ead6da46dc0ce26343defc6f9607987ce0ecad5073e48c71f21d1a198cd68600a4c434dca26310460999c564885b6901c6f59ec3db84bd8e7adede27c5fdb270042a57d50415afe509c0c88edc5c611ca6f63bed63c88714ed56987ee3ca8f",
			"aggregate": {
				"aggregation_bits": "0x0000000000000000000004000000000000000010000000000000081000000000",
				"data": {
					"slot": "99328",
					"index": "0",
					"beacon_block_root": "0x1b15069e9e02e105eeabbb68418850f1188dc8b299e2321f13582a9a62910447",
					"source": {
						"epoch": "3103",
						"root": "0x443c834c80f35ed83d973bc75206744b91c4857599b236140cb0e69d61c86b16"
					},
					"target": {
						"epoch": "3104",
						"root": "0x1b15069e9e02e105eeabbb68418850f1188dc8b299e2321f13582a9a62910447"
					}
				},
				"signature": "0x9426132ed6eb3deffdf87651fcb9063b4d70a7e128998c62373b64abac85f3a6e04a86127d59a1acce4aef684633e287167cb866df4e7ba4e9b269bada18dc299cdd92c9a1988372a107dc691ac085a9191881ba45dd92706419d4e5ebd23f5f",
				"committee_bits": "0xfeff000000000000"
			}
		}`)

		var result AggregateAndProof
		err := json.Unmarshal(jsonData, &result)

		require.NoError(t, err)
		require.NotNil(t, result.Electra)
		require.Nil(t, result.Phase0)
		require.Equal(t, phase0.ValidatorIndex(123), result.Electra.AggregatorIndex)
		require.Equal(t, phase0.Slot(99328), result.Electra.Aggregate.Data.Slot)
		require.NotNil(t, result.Electra.Aggregate.CommitteeBits)
		require.Equal(t, byte(0xfe), result.Electra.Aggregate.CommitteeBits[0])
	})

	// Test case 2: JSON without "committee_bits" gets unmarshaled to Phase0
	t.Run("Unmarshal JSON without committee_bits to Phase0", func(t *testing.T) {
		t.Parallel()

		jsonData := []byte(`{
			"aggregator_index": "402",
			"selection_proof": "0x8b5f33a895612754103fbaaed74b408e89b948c69740d722b56207c272e001b2ddd445931e40a2938c84afab86c2606f0c1a93a0aaf4962c91d3ddf309de8ef0dbd68f590573e53e5ff7114e9625fae2cfee9e7eb991ad929d351c7701581d9c",
			"aggregate": {
				"aggregation_bits": "0xffffffff01",
				"data": {
					"slot": "66",
					"index": "0",
					"beacon_block_root": "0x737b2949b471552a7f95f772e289ae6d74bd8e527120d9993095fd34ed89e100",
					"source": {
						"epoch": "0",
						"root": "0x443c834c80f35ed83d973bc75206744b91c4857599b236140cb0e69d61c86b16"
					},
					"target": {
						"epoch": "2",
						"root": "0x674d7e0ce7a28ba0d71ecef8d44621e8f4ed206e9116dc647fafd7f32f61f440"
					}
				},
				"signature": "0x8a75731b877a4be72ddc81ae5318eaa9863fef2297b58a4f01a447bd1fff10d48bb79e62d280557c472af5d457032e0112db17f99b2e925ce2c89dd839e5bd8e5e95b2f5253bb80087753555c69b116162c334f5a142e38ff6a66ef579c9a70d"
			}
		}`)

		var result AggregateAndProof
		err := json.Unmarshal(jsonData, &result)

		require.NoError(t, err)
		require.NotNil(t, result.Phase0)
		require.Nil(t, result.Electra)
		require.Equal(t, phase0.ValidatorIndex(402), result.Phase0.AggregatorIndex)
		require.Equal(t, phase0.Slot(66), result.Phase0.Aggregate.Data.Slot)
	})
}
