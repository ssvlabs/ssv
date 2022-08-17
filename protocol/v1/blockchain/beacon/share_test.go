package beacon

import (
	"testing"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"
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
			share := &Share{
				NodeID:    0,
				PublicKey: nil,
				Metadata:  nil,
				Committee: map[spectypes.OperatorID]*Node{},
			}
			// compile committee
			for i := uint64(1); i <= test.committeeSize; i++ {
				share.Committee[spectypes.OperatorID(i)] = &Node{}
			}

			require.EqualValues(t, test.expectedThreshold, share.ThresholdSize())
			require.EqualValues(t, test.expectedPartialThreshold, share.PartialThresholdSize())
		})
	}
}

func TestShare_HashOperators(t *testing.T) {
	share := &Share{
		NodeID:    0,
		PublicKey: nil,
		Metadata:  nil,
		Committee: map[spectypes.OperatorID]*Node{},
		Operators: make([][]byte, 4),
	}
	for i := spectypes.OperatorID(1); i <= 4; i++ {
		share.Committee[i] = &Node{}
		share.Operators[int(i-1)] = []byte{byte(i)}
	}

	hashes := share.HashOperators()
	require.Len(t, hashes, 4)
}
