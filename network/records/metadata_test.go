package records

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNodeMetadata_Encode(t *testing.T) {
	tests := []struct {
		name      string
		subnets   string
		errString string
	}{
		{
			name:      "Subnets too short",
			subnets:   strings.Repeat("0", 31),
			errString: "invalid subnets length 31",
		},
		{
			name:      "Subnets too long",
			subnets:   strings.Repeat("0", 33),
			errString: "invalid subnets length 33",
		},
		{
			name:      "Subnets exact and valid",
			subnets:   strings.Repeat("0", 32),
			errString: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodeMeta := NodeMetadata{
				NodeVersion:   "1.0.0",
				ExecutionNode: "geth/v1.10.8",
				ConsensusNode: "lighthouse/v1.5.0",
				Subnets:       tt.subnets,
			}

			_, err := nodeMeta.Encode()
			if tt.errString != "" {
				require.EqualError(t, err, tt.errString)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNodeMetadata_Decode(t *testing.T) {
	tests := []struct {
		name      string
		encoded   string
		errString string
	}{
		{
			name:      "Subnets too short",
			encoded:   `{"NodeVersion":"1.0.0","ExecutionNode":"geth/v1.10.8","ConsensusNode":"lighthouse/v1.5.0","Subnets":"1234123412341234123412341234123"}`,
			errString: "invalid subnets length 31",
		},
		{
			name:      "Subnets too long",
			encoded:   `{"NodeVersion":"1.0.0","ExecutionNode":"geth/v1.10.8","ConsensusNode":"lighthouse/v1.5.0","Subnets":"123412341234123412341234123412341"}`,
			errString: "invalid subnets length 33",
		},
		{
			name:      "Subnets exact and valid",
			encoded:   `{"NodeVersion":"1.0.0","ExecutionNode":"geth/v1.10.8","ConsensusNode":"lighthouse/v1.5.0","Subnets":"12341234123412341234123412341234"}`,
			errString: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodeMeta := NodeMetadata{}

			err := nodeMeta.Decode([]byte(tt.encoded))
			if tt.errString != "" {
				require.EqualError(t, err, tt.errString)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
