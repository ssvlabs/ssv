package format

import (
	"testing"

	spectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"github.com/stretchr/testify/require"
)

func TestDomainTypeFromString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    spectypes.DomainType
		wantErr bool
	}{
		{
			name:    "valid",
			input:   "0x00000301",
			want:    spectypes.DomainType{0x0, 0x0, 0x3, 0x1},
			wantErr: false,
		},
		{
			name:    "valid without 0x prefix",
			input:   "00000301",
			want:    spectypes.DomainType{0x0, 0x0, 0x3, 0x1},
			wantErr: false,
		},
		{
			name:    "invalid length",
			input:   "000003011",
			want:    spectypes.DomainType{0x0, 0x0, 0x3, 0x1},
			wantErr: true,
		},
		{
			name:    "invalid hex",
			input:   "0000030g",
			want:    spectypes.DomainType{0x0, 0x0, 0x3, 0x1},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got, err := DomainTypeFromString(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, spectypes.DomainType(got))
		})
	}
}

func TestIdentifierUnformat(t *testing.T) {
	pk, role := IdentifierUnformat("xxx_ATTESTER")
	require.Equal(t, "xxx", pk)
	require.Equal(t, "ATTESTER", role)
}

func TestIdentifierFormat(t *testing.T) {
	identifier := IdentifierFormat([]byte("111"), "ATTESTER")
	require.Equal(t, "313131_ATTESTER", identifier)
}
