package signedmsg

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/protocol/v1/message"
)

func TestBasicMsgValidation(t *testing.T) {
	pipeline := BasicMsgValidation()

	err := pipeline.Run(nil)
	require.EqualError(t, err, "signed message is nil")

	err = pipeline.Run(&message.SignedMessage{})
	require.EqualError(t, err, "message body is nil")

	err = pipeline.Run(&message.SignedMessage{
		Message: &message.ConsensusMessage{},
	})
	require.NoError(t, err)
}
