package qbft

import (
	"encoding/hex"
	"github.com/bloxapp/ssv-spec/qbft"
	spectests "github.com/bloxapp/ssv-spec/qbft/spectest/tests"
	"github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"testing"
)

func RunCreateMsg(t *testing.T, test *spectests.CreateMsgSpecTest) {
	var msg *qbft.SignedMessage
	var lastErr error
	switch test.CreateType {
	case spectests.CreateProposal:
		msg, lastErr = createProposal(test)
	case spectests.CreatePrepare:
		msg, lastErr = createPrepare(test)
	case spectests.CreateCommit:
		msg, lastErr = createCommit(test)
	case spectests.CreateRoundChange:
		msg, lastErr = createRoundChange(test)
	default:
		t.Fail()
	}

	r, err := msg.GetRoot()
	if err != nil {
		lastErr = err
	}

	if len(test.ExpectedError) != 0 {
		require.EqualError(t, lastErr, test.ExpectedError)
	} else {
		require.NoError(t, lastErr)
	}

	require.EqualValues(t, test.ExpectedRoot, hex.EncodeToString(r))
}

func createCommit(test *spectests.CreateMsgSpecTest) (*qbft.SignedMessage, error) {
	ks := testingutils.Testing4SharesSet()
	state := &qbft.State{
		Share: testingutils.TestingShare(ks),
		ID:    []byte{1, 2, 3, 4},
	}
	config := testingutils.TestingConfig(ks)

	return qbft.CreateCommit(state, config, test.Value)
}

func createPrepare(test *spectests.CreateMsgSpecTest) (*qbft.SignedMessage, error) {
	ks := testingutils.Testing4SharesSet()
	state := &qbft.State{
		Share: testingutils.TestingShare(ks),
		ID:    []byte{1, 2, 3, 4},
	}
	config := testingutils.TestingConfig(ks)

	return qbft.CreatePrepare(state, config, test.Round, test.Value)
}

func createProposal(test *spectests.CreateMsgSpecTest) (*qbft.SignedMessage, error) {
	ks := testingutils.Testing4SharesSet()
	state := &qbft.State{
		Share: testingutils.TestingShare(ks),
		ID:    []byte{1, 2, 3, 4},
	}
	config := testingutils.TestingConfig(ks)

	return qbft.CreateProposal(state, config, test.Value, test.RoundChangeJustifications, test.PrepareJustifications)
}

func createRoundChange(test *spectests.CreateMsgSpecTest) (*qbft.SignedMessage, error) {
	ks := testingutils.Testing4SharesSet()
	state := &qbft.State{
		Share: testingutils.TestingShare(ks),
		ID:    []byte{1, 2, 3, 4},
	}
	config := testingutils.TestingConfig(ks)

	if len(test.PrepareJustifications) > 0 {
		state.LastPreparedRound = test.PrepareJustifications[0].Message.Round
		state.LastPreparedValue = test.Value

		for _, msg := range test.PrepareJustifications {
			_, err := state.PrepareContainer.AddFirstMsgForSignerAndRound(msg)
			if err != nil {
				return nil, errors.Wrap(err, "could not add first message for signer")
			}
		}
	}

	return qbft.CreateRoundChange(state, config, 1, test.Value)
}
