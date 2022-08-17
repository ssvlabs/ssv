package signedmsg

import (
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/stretchr/testify/require"
)

func TestBasicMsgValidation(t *testing.T) {
	pipeline := BasicMsgValidation()

	err := pipeline.Run(nil)
	require.EqualError(t, err, "signed message is nil")

	err = pipeline.Run(&specqbft.SignedMessage{})
	require.EqualError(t, err, "message body is nil")

	err = pipeline.Run(&specqbft.SignedMessage{
		Message: &specqbft.Message{},
	})
	require.NoError(t, err)
}
