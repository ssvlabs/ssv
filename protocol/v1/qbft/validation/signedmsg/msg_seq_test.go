package signedmsg

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/protocol/v1/message"
)

func TestMsgSeq(t *testing.T) {
	tests := []struct {
		name          string
		expectedSeq   message.Height
		actualSeq     message.Height
		expectedError string
	}{
		{
			"valid",
			1,
			1,
			"",
		},
		{
			"different msg seq",
			1,
			2,
			"invalid message sequence number: expected: 1, actual: 2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pipeline := ValidateSequenceNumber(test.expectedSeq)
			err := pipeline.Run(&message.SignedMessage{
				Message: &message.ConsensusMessage{
					Height: test.actualSeq,
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
