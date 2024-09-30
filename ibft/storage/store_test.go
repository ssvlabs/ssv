package storage

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
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
