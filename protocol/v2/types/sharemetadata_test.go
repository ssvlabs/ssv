package types

import (
	"testing"

	"github.com/stretchr/testify/require"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
)

func TestShareMetadata_BelongsToOperatorID(t *testing.T) {
	metadata := &ShareMetadata{
		OperatorIDs: []uint64{1, 2, 3, 4},
	}

	require.True(t, metadata.BelongsToOperatorID(1))
	require.False(t, metadata.BelongsToOperatorID(10))
}

func TestShare_IsOperatorShare(t *testing.T) {
	metadata := &ShareMetadata{
		Operators: [][]byte{
			{1, 1, 1, 1},
			{2, 2, 2, 2},
		},
	}

	require.True(t, metadata.BelongsToOperator(string([]byte{1, 1, 1, 1})))
	require.False(t, metadata.BelongsToOperator(string([]byte{1, 2, 3, 4})))
}

func TestShare_HasStats(t *testing.T) {
	tt := []struct {
		Name     string
		Metadata *ShareMetadata
		Result   bool
	}{
		{
			Name:     "ShareMetadata == nil",
			Metadata: nil,
			Result:   false,
		},
		{
			Name: "Stats == nil",
			Metadata: &ShareMetadata{
				Stats: nil,
			},
			Result: false,
		},
		{
			Name: "Stats != nil",
			Metadata: &ShareMetadata{
				Stats: &beaconprotocol.ValidatorMetadata{},
			},
			Result: true,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			require.Equal(t, tc.Result, tc.Metadata.HasStats())
		})
	}
}
