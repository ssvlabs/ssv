package signedmsg

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/protocol/v1/message"
)

func TestMsgTypeCheck(t *testing.T) {
	tests := []struct {
		name          string
		expectedType  message.ConsensusMessageType
		actualType    message.ConsensusMessageType
		expectedError string
	}{
		{
			"valid",
			message.PrepareMsgType,
			message.PrepareMsgType,
			"",
		},
		{
			"different round state",
			message.PrepareMsgType,
			message.DecidedMsgType,
			"message type is wrong",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pipeline := MsgTypeCheck(test.expectedType)
			err := pipeline.Run(&message.SignedMessage{
				Message: &message.ConsensusMessage{
					MsgType: test.actualType,
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
