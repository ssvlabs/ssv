package web3signer

import (
	"encoding/json"
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/stretchr/testify/require"
)

func TestDataVersion_MarshalJSON(t *testing.T) {
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
			output, err := json.Marshal(&tt.input)
			require.NoError(t, err)
			require.Equal(t, tt.expected, string(output))
		})
	}
}

func TestDataVersion_UnmarshalJSON(t *testing.T) {
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
			var dv DataVersion
			err := json.Unmarshal([]byte(tt.input), &dv)
			require.NoError(t, err)
			require.Equal(t, tt.expected, dv)
		})
	}
}

func TestDataVersion_RoundTrip(t *testing.T) {
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
