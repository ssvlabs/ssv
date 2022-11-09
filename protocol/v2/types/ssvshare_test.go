package types

import (
	"testing"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
)

func TestShareMetadata_BelongsToOperatorID(t *testing.T) {
	metadata := &SSVShare{
		Share: spectypes.Share{
			Committee: []*spectypes.Operator{
				{
					OperatorID: 1,
					PubKey:     []byte{1, 1, 1, 1},
				},
				{
					OperatorID: 2,
					PubKey:     []byte{2, 2, 2, 2},
				},
				{
					OperatorID: 3,
					PubKey:     []byte{3, 3, 3, 3},
				},
				{
					OperatorID: 4,
					PubKey:     []byte{4, 4, 4, 4},
				},
			},
		},
	}

	require.True(t, metadata.BelongsToOperatorID(1))
	require.False(t, metadata.BelongsToOperatorID(10))
}

func TestShare_IsOperatorShare(t *testing.T) {
	metadata := &SSVShare{
		ShareMetadata: ShareMetadata{
			Operators: [][]byte{
				{1, 1, 1, 1},
				{2, 2, 2, 2},
			},
		},
	}

	require.True(t, metadata.BelongsToOperator(string([]byte{1, 1, 1, 1})))
	require.False(t, metadata.BelongsToOperator(string([]byte{1, 2, 3, 4})))
}

func TestShare_HasStats(t *testing.T) {
	tt := []struct {
		Name          string
		ShareMetadata *SSVShare
		Result        bool
	}{
		{
			Name:          "ShareMetadata == nil",
			ShareMetadata: nil,
			Result:        false,
		},
		{
			Name: "Stats == nil",
			ShareMetadata: &SSVShare{
				ShareMetadata: ShareMetadata{
					Stats: nil,
				},
			},
			Result: false,
		},
		{
			Name: "Stats != nil",
			ShareMetadata: &SSVShare{
				ShareMetadata: ShareMetadata{
					Stats: &beaconprotocol.ValidatorMetadata{},
				},
			},
			Result: true,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			require.Equal(t, tc.Result, tc.ShareMetadata.HasStats())
		})
	}
}
