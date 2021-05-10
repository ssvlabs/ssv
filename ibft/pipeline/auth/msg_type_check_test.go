package auth

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMsgTypeCheck(t *testing.T) {
	tests := []struct {
		name          string
		expectedType  proto.RoundState
		actualType    proto.RoundState
		expectedError string
	}{
		{
			"valid",
			proto.RoundState_Prepare,
			proto.RoundState_Prepare,
			"",
		},
		{
			"different round state",
			proto.RoundState_Prepare,
			proto.RoundState_Decided,
			"message type is wrong",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pipeline := MsgTypeCheck(test.expectedType)
			err := pipeline.Run(&proto.SignedMessage{
				Message: &proto.Message{
					Type: test.actualType,
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
