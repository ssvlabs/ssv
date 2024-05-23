package qbft

import (
	"testing"

	"github.com/ssvlabs/ssv-spec/qbft"
	"github.com/stretchr/testify/require"

	spectests "github.com/ssvlabs/ssv-spec/qbft/spectest/tests"
)

func RunMsg(t *testing.T, test *spectests.MsgSpecTest) { // using only spec struct so this test can be imported
	var lastErr error

	for i, msg := range test.Messages {
		if err := msg.Validate(); err != nil {
			lastErr = err
			continue
		}

		qbftMessage := &qbft.Message{}
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

	// check error
	if len(test.ExpectedError) != 0 {
		t.Log("Expected error", test.ExpectedError)
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}
}
