package signedmsg

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/protocol/v1/message"
)

func TestMsgLambda(t *testing.T) {
	tests := []struct {
		name           string
		expectedLambda []byte
		actualLambda   []byte
		expectedError  string
	}{
		{
			"valid",
			[]byte{1, 2, 3, 4},
			[]byte{1, 2, 3, 4},
			"",
		},
		{
			"different msg lambda",
			[]byte{1, 2, 3, 4},
			[]byte{1, 2, 3, 3},
			"message Lambda (\x01\x02\x03\x03) does not equal expected Lambda (\x01\x02\x03\x04)",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pipeline := ValidateLambdas(test.expectedLambda)
			err := pipeline.Run(&message.SignedMessage{
				Message: &message.ConsensusMessage{
					Identifier: test.actualLambda,
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
