package signedmsg

import (
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/stretchr/testify/require"
)

func TestMsgTypeCheck(t *testing.T) {
	tests := []struct {
		name          string
		expectedType  specqbft.MessageType
		actualType    specqbft.MessageType
		expectedError string
	}{
		{
			"valid",
			specqbft.PrepareMsgType,
			specqbft.PrepareMsgType,
			"",
		},
		{
			"different round state",
			specqbft.PrepareMsgType,
			specqbft.ProposalMsgType,
			"message type is wrong",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pipeline := MsgTypeCheck(test.expectedType)
			err := pipeline.Run(&specqbft.SignedMessage{
				Message: &specqbft.Message{
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
