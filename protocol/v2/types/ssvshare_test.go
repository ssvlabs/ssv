package types

import (
	"testing"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

func TestSSVShare_BelongsToOperator(t *testing.T) {
	metadata := &SSVShare{
		Share: spectypes.Share{
			OperatorID: 1,
		},
	}

	require.True(t, metadata.BelongsToOperator(1))
	require.False(t, metadata.BelongsToOperator(2))
}

func TestSSVShare_HasBeaconMetadata(t *testing.T) {
	tt := []struct {
		Name          string
		ShareMetadata *SSVShare
		Result        bool
	}{
		{
			Name:          "Metadata == nil",
			ShareMetadata: nil,
			Result:        false,
		},
		{
			Name: "BeaconMetadata == nil",
			ShareMetadata: &SSVShare{
				Metadata: Metadata{
					BeaconMetadata: nil,
				},
			},
			Result: false,
		},
		{
			Name: "BeaconMetadata != nil",
			ShareMetadata: &SSVShare{
				Metadata: Metadata{
					BeaconMetadata: &beaconprotocol.ValidatorMetadata{},
				},
			},
			Result: true,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			require.Equal(t, tc.Result, tc.ShareMetadata.HasBeaconMetadata())
		})
	}
}
