package qbft

import (
	"github.com/bloxapp/ssv-spec/qbft"
	spectests "github.com/bloxapp/ssv-spec/qbft/spectest/tests"
	"github.com/stretchr/testify/require"
	"testing"
)

func RunMsg(t *testing.T, test *spectests.MsgSpecTest) { // using only spec struct so this test can be imported
	var lastErr error

	for i, msg := range test.Messages {
		if err := msg.Validate(); err != nil {
			lastErr = err
			continue
		}

		switch msg.Message.MsgType {
		case qbft.RoundChangeMsgType:
			rc := qbft.RoundChangeData{}
			if err := rc.Decode(msg.Message.Data); err != nil {
				lastErr = err
			}
			if err := rc.Validate(); err != nil {
				lastErr = err
			}
		case qbft.CommitMsgType:
			rc := qbft.CommitData{}
			if err := rc.Decode(msg.Message.Data); err != nil {
				lastErr = err
			}
			if err := rc.Validate(); err != nil {
				lastErr = err
			}
		case qbft.PrepareMsgType:
			rc := qbft.PrepareData{}
			if err := rc.Decode(msg.Message.Data); err != nil {
				lastErr = err
			}
			if err := rc.Validate(); err != nil {
				lastErr = err
			}
		case qbft.ProposalMsgType:
			rc := qbft.ProposalData{}
			if err := rc.Decode(msg.Message.Data); err != nil {
				lastErr = err
			}
			if err := rc.Validate(); err != nil {
				lastErr = err
			}
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
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}
}
