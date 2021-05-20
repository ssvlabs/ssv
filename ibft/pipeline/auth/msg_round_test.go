package auth

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMsgRound(t *testing.T) {
	tests := []struct {
		name          string
		expectedRound uint64
		actualRound   uint64
		expectedError string
	}{
		{
			"valid",
			1,
			1,
			"",
		},
		{
			"different msg rounds",
			1,
			2,
			"message round (2) does not equal State round (1)",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pipeline := ValidateRound(test.expectedRound)
			err := pipeline.Run(&proto.SignedMessage{
				Message: &proto.Message{
					Round: test.actualRound,
				},
			})

			if len(test.expectedError) == 0 {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, test.expectedError)
			}
		})
	}
}
