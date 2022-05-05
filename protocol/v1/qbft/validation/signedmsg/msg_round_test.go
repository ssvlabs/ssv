package signedmsg

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/protocol/v1/message"
)

func TestMsgRound(t *testing.T) {
	tests := []struct {
		name          string
		expectedRound message.Round
		actualRound   message.Round
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
			"message round (2) does not equal state round (1)",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pipeline := ValidateRound(test.expectedRound)
			err := pipeline.Run(&message.SignedMessage{
				Message: &message.ConsensusMessage{
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
