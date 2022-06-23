package qbft_test

import (
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/spec/qbft"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/bloxapp/ssv/spec/types/testingutils"
)

func TestUponCommit(t *testing.T) {
	t.Run("single commit", func(t *testing.T) {
		i := testingutils.BaseInstance()
		i.State.ProposalAcceptedForCurrentRound = testingutils.SignQBFTMsg(testingutils.Testing4SharesSet().Shares[1], types.OperatorID(1), &qbft.Message{
			MsgType:    qbft.ProposalMsgType,
			Height:     qbft.FirstHeight,
			Round:      qbft.FirstRound,
			Identifier: []byte{1, 2, 3, 4},
			Data:       testingutils.ProposalDataBytes([]byte{1, 2, 3, 4}, nil, nil),
		})
		msg := testingutils.SignQBFTMsg(testingutils.Testing4SharesSet().Shares[1], types.OperatorID(1), &qbft.Message{
			MsgType:    qbft.CommitMsgType,
			Height:     qbft.FirstHeight,
			Round:      qbft.FirstRound,
			Identifier: []byte{1, 2, 3, 4},
			Data:       testingutils.CommitDataBytes([]byte{1, 2, 3, 4}),
		})
		// TODO(nkryuchkov): i?
		_, _, _, err := i.UponCommit(msg, i.State.CommitContainer)
		//_, _, _, err := qbft.UponCommit(i.State, testingutils.TestingConfig(testingutils.Testing4SharesSet()), msg, i.State.CommitContainer)
		require.NoError(t, err)
	})

	t.Run("single commits to quorum", func(t *testing.T) {
		i := testingutils.BaseInstance()
		i.State.ProposalAcceptedForCurrentRound = testingutils.SignQBFTMsg(testingutils.Testing4SharesSet().Shares[1], types.OperatorID(1), &qbft.Message{
			MsgType:    qbft.ProposalMsgType,
			Height:     qbft.FirstHeight,
			Round:      qbft.FirstRound,
			Identifier: []byte{1, 2, 3, 4},
			Data:       testingutils.ProposalDataBytes([]byte{1, 2, 3, 4}, nil, nil),
		})

		msg := testingutils.SignQBFTMsg(testingutils.Testing4SharesSet().Shares[1], types.OperatorID(1), &qbft.Message{
			MsgType:    qbft.CommitMsgType,
			Height:     qbft.FirstHeight,
			Round:      qbft.FirstRound,
			Identifier: []byte{1, 2, 3, 4},
			Data:       testingutils.CommitDataBytes([]byte{1, 2, 3, 4}),
		})
		// TODO(nkryuchkov): i?
		decided, _, _, err := i.UponCommit(msg, i.State.CommitContainer)
		//decided, _, _, err := qbft.UponCommit(i.State, testingutils.TestingConfig(testingutils.Testing4SharesSet()), msg, i.State.CommitContainer)
		require.NoError(t, err)
		require.False(t, decided)

		msg = testingutils.SignQBFTMsg(testingutils.Testing4SharesSet().Shares[2], types.OperatorID(2), &qbft.Message{
			MsgType:    qbft.CommitMsgType,
			Height:     qbft.FirstHeight,
			Round:      qbft.FirstRound,
			Identifier: []byte{1, 2, 3, 4},
			Data:       testingutils.CommitDataBytes([]byte{1, 2, 3, 4}),
		})
		// TODO(nkryuchkov): i?
		decided, _, _, err = i.UponCommit(msg, i.State.CommitContainer)
		//decided, _, _, err = qbft.UponCommit(i.State, testingutils.TestingConfig(testingutils.Testing4SharesSet()), msg, i.State.CommitContainer)
		require.NoError(t, err)
		require.False(t, decided)

		msg = testingutils.SignQBFTMsg(testingutils.Testing4SharesSet().Shares[3], types.OperatorID(3), &qbft.Message{
			MsgType:    qbft.CommitMsgType,
			Height:     qbft.FirstHeight,
			Round:      qbft.FirstRound,
			Identifier: []byte{1, 2, 3, 4},
			Data:       testingutils.CommitDataBytes([]byte{1, 2, 3, 4}),
		})
		// TODO(nkryuchkov): i?
		decided, _, _, err = i.UponCommit(msg, i.State.CommitContainer)
		//decided, _, _, err = qbft.UponCommit(i.State, testingutils.TestingConfig(testingutils.Testing4SharesSet()), msg, i.State.CommitContainer)
		require.NoError(t, err)
		require.True(t, decided)
	})

	t.Run("multi signer to quorum", func(t *testing.T) {
		i := testingutils.BaseInstance()
		i.State.ProposalAcceptedForCurrentRound = testingutils.SignQBFTMsg(testingutils.Testing4SharesSet().Shares[1], types.OperatorID(1), &qbft.Message{
			MsgType:    qbft.ProposalMsgType,
			Height:     qbft.FirstHeight,
			Round:      qbft.FirstRound,
			Identifier: []byte{1, 2, 3, 4},
			Data:       testingutils.ProposalDataBytes([]byte{1, 2, 3, 4}, nil, nil),
		})

		msg := testingutils.MultiSignQBFTMsg([]*bls.SecretKey{testingutils.Testing4SharesSet().Shares[1], testingutils.Testing4SharesSet().Shares[2]}, []types.OperatorID{1, 2}, &qbft.Message{
			MsgType:    qbft.CommitMsgType,
			Height:     qbft.FirstHeight,
			Round:      qbft.FirstRound,
			Identifier: []byte{1, 2, 3, 4},
			Data:       testingutils.CommitDataBytes([]byte{1, 2, 3, 4}),
		})
		// TODO(nkryuchkov): i?
		decided, _, _, err := i.UponCommit(msg, i.State.CommitContainer)
		//decided, _, _, err := qbft.UponCommit(i.State, testingutils.TestingConfig(testingutils.Testing4SharesSet()), msg, i.State.CommitContainer)
		require.NoError(t, err)
		require.False(t, decided)

		msg = testingutils.SignQBFTMsg(testingutils.Testing4SharesSet().Shares[3], types.OperatorID(3), &qbft.Message{
			MsgType:    qbft.CommitMsgType,
			Height:     qbft.FirstHeight,
			Round:      qbft.FirstRound,
			Identifier: []byte{1, 2, 3, 4},
			Data:       testingutils.CommitDataBytes([]byte{1, 2, 3, 4}),
		})
		// TODO(nkryuchkov): i?
		decided, _, _, err = i.UponCommit(msg, i.State.CommitContainer)
		//decided, _, _, err = qbft.UponCommit(i.State, testingutils.TestingConfig(testingutils.Testing4SharesSet()), msg, i.State.CommitContainer)
		require.NoError(t, err)
		require.True(t, decided)
	})

	t.Run("multi signer to quorum", func(t *testing.T) {
		i := testingutils.BaseInstance()
		i.State.ProposalAcceptedForCurrentRound = testingutils.SignQBFTMsg(testingutils.Testing4SharesSet().Shares[1], types.OperatorID(1), &qbft.Message{
			MsgType:    qbft.ProposalMsgType,
			Height:     qbft.FirstHeight,
			Round:      qbft.FirstRound,
			Identifier: []byte{1, 2, 3, 4},
			Data:       testingutils.ProposalDataBytes([]byte{1, 2, 3, 4}, nil, nil),
		})

		msg := testingutils.MultiSignQBFTMsg([]*bls.SecretKey{testingutils.Testing4SharesSet().Shares[1], testingutils.Testing4SharesSet().Shares[2]}, []types.OperatorID{1, 2}, &qbft.Message{
			MsgType:    qbft.CommitMsgType,
			Height:     qbft.FirstHeight,
			Round:      qbft.FirstRound,
			Identifier: []byte{1, 2, 3, 4},
			Data:       testingutils.CommitDataBytes([]byte{1, 2, 3, 4}),
		})
		// TODO(nkryuchkov): i?
		decided, _, _, err := i.UponCommit(msg, i.State.CommitContainer)
		// decided, _, _, err := qbft.UponCommit(i.State, testingutils.TestingConfig(testingutils.Testing4SharesSet()), msg, i.State.CommitContainer)
		require.NoError(t, err)
		require.False(t, decided)

		msg = testingutils.MultiSignQBFTMsg([]*bls.SecretKey{testingutils.Testing4SharesSet().Shares[3], testingutils.Testing4SharesSet().Shares[4]}, []types.OperatorID{3, 4}, &qbft.Message{
			MsgType:    qbft.CommitMsgType,
			Height:     qbft.FirstHeight,
			Round:      qbft.FirstRound,
			Identifier: []byte{1, 2, 3, 4},
			Data:       testingutils.CommitDataBytes([]byte{1, 2, 3, 4}),
		})
		// TODO(nkryuchkov): i?
		decided, _, _, err = i.UponCommit(msg, i.State.CommitContainer)
		//decided, _, _, err = qbft.UponCommit(i.State, testingutils.TestingConfig(testingutils.Testing4SharesSet()), msg, i.State.CommitContainer)
		require.NoError(t, err)
		require.True(t, decided)
	})

}
