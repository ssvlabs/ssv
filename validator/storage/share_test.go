package storage

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/protocol/v1/validator/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestThresholdSize(t *testing.T) {
	tests := []struct {
		name                     string
		committeeSize            uint64
		expectedThreshold        int
		expectedPartialThreshold int
	}{
		{
			"committee of 4",
			4,
			3,
			2,
		},
		{
			"committee of 7",
			7,
			5,
			3,
		},
		{
			"committee of 10",
			10,
			7,
			4,
		},
		{
			"committee of 13",
			13,
			9,
			5,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			share := &types.Share{
				NodeID:    0,
				PublicKey: nil,
				Metadata:  nil,
				Committee: map[uint64]*proto.Node{},
			}
			// compile committee
			for i := uint64(1); i <= test.committeeSize; i++ {
				share.Committee[i] = &proto.Node{}
			}

			require.EqualValues(t, test.expectedThreshold, share.ThresholdSize())
			require.EqualValues(t, test.expectedPartialThreshold, share.PartialThresholdSize())
		})
	}
}

func TestShare_HashOperators(t *testing.T) {
	share := &types.Share{
		NodeID:    0,
		PublicKey: nil,
		Metadata:  nil,
		Committee: map[uint64]*proto.Node{},
		Operators: make([][]byte, 4),
	}
	for i := uint64(1); i <= 4; i++ {
		share.Committee[i] = &proto.Node{}
		share.Operators[int(i-1)] = []byte{byte(i)}
	}

	hashes := share.HashOperators()
	require.Len(t, hashes, 4)
}
