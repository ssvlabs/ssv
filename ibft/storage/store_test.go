package storage

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
)

func TestEncodeDecodeOperators(t *testing.T) {
	testCases := []struct {
		input   []uint64
		encoded []byte
	}{
		// Valid sizes: 4
		{[]uint64{0x0123456789ABCDEF, 0xFEDCBA9876543210, 0x1122334455667788, 0x8877665544332211},
			[]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0xFE, 0xDC, 0xBA, 0x98, 0x76, 0x54, 0x32, 0x10, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11}},
		// Valid sizes: 7
		{[]uint64{1, 2, 3, 4, 5, 6, 7},
			[]byte{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 3, 0, 0, 0, 0, 0, 0, 0, 4, 0, 0, 0, 0, 0, 0, 0, 5, 0, 0, 0, 0, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0, 7}},
		// Valid sizes: 13
		{[]uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12},
			[]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 3, 0, 0, 0, 0, 0, 0, 0, 4, 0, 0, 0, 0, 0, 0, 0, 5, 0, 0, 0, 0, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0, 7, 0, 0, 0, 0, 0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 9, 0, 0, 0, 0, 0, 0, 0, 10, 0, 0, 0, 0, 0, 0, 0, 11, 0, 0, 0, 0, 0, 0, 0, 12}},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("Case %d", i+1), func(t *testing.T) {
			encoded, err := encodeOperators(tc.input)
			require.Equal(t, err, nil)
			require.Equal(t, tc.encoded, encoded)

			decoded := decodeOperators(encoded)
			require.Equal(t, tc.input, decoded)
		})
	}
}

func Test_mergeParticipantsBitMask(t *testing.T) {
	tests := []struct {
		name          string
		participants1 []spectypes.OperatorID
		participants2 []spectypes.OperatorID
		expected      []spectypes.OperatorID
	}{
		{
			name:          "Both participants empty",
			participants1: []spectypes.OperatorID{},
			participants2: []spectypes.OperatorID{},
			expected:      []spectypes.OperatorID{},
		},
		{
			name:          "First participants empty",
			participants1: []spectypes.OperatorID{},
			participants2: []spectypes.OperatorID{1, 2, 3},
			expected:      []spectypes.OperatorID{1, 2, 3},
		},
		{
			name:          "Second participants empty",
			participants1: []spectypes.OperatorID{1, 2, 3},
			participants2: []spectypes.OperatorID{},
			expected:      []spectypes.OperatorID{1, 2, 3},
		},
		{
			name:          "No duplicates",
			participants1: []spectypes.OperatorID{1, 3, 5},
			participants2: []spectypes.OperatorID{2, 4, 6},
			expected:      []spectypes.OperatorID{1, 2, 3, 4, 5, 6},
		},
		{
			name:          "With duplicates",
			participants1: []spectypes.OperatorID{1, 2, 3, 5},
			participants2: []spectypes.OperatorID{3, 4, 5, 6},
			expected:      []spectypes.OperatorID{1, 2, 3, 4, 5, 6},
		},
		{
			name:          "All duplicates",
			participants1: []spectypes.OperatorID{1, 2, 3},
			participants2: []spectypes.OperatorID{1, 2, 3},
			expected:      []spectypes.OperatorID{1, 2, 3},
		},
		{
			name:          "Large participants size",
			participants1: []spectypes.OperatorID{1, 3, 5, 7, 9, 11, 13},
			participants2: []spectypes.OperatorID{2, 4, 6, 8, 10, 12},
			expected:      []spectypes.OperatorID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13},
		},
	}

	for _, tt := range tests {
		committee := []spectypes.OperatorID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}

		t.Run(tt.name, func(t *testing.T) {
			q1 := qbftstorage.Quorum{
				Signers:   tt.participants1,
				Committee: committee,
			}

			q2 := qbftstorage.Quorum{
				Signers:   tt.participants2,
				Committee: committee,
			}

			result := mergeParticipantsBitMask(q1.ToSignersBitMask(), q2.ToSignersBitMask())
			require.Equal(t, tt.expected, result.Signers(committee))
		})
	}
}
