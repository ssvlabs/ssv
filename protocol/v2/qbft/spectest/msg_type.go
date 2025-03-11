package qbft

import (
	"testing"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectests "github.com/ssvlabs/ssv-spec/qbft/spectest/tests"
	"github.com/stretchr/testify/require"
)

func RunMsg(t *testing.T, test *spectests.MsgSpecTest) { // using only spec struct so this test can be imported
	var lastErr error
	for i, msg := range test.Messages {
		if err := msg.Validate(); err != nil {
			lastErr = err
			continue
		}

		qbftMessage := &specqbft.Message{}
		require.NoError(t, qbftMessage.Decode(msg.SSVMessage.Data))
		if err := qbftMessage.Validate(); err != nil {
			lastErr = err
			continue
		}

		if len(test.EncodedMessages) > 0 {
			byts, err := msg.Encode()
			require.NoError(t, err)
			require.EqualValues(t, test.EncodedMessages[i], byts)
		}

		if len(test.ExpectedRoots) > 0 {
			r, err := msg.GetRoot()
			require.NoError(t, err)
			require.EqualValues(t, test.ExpectedRoots[i], r)
		}
	}
	if test.ExpectedError != "" {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}
}
