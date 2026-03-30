package instance

import (
	"context"
	"testing"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestUponPrepareBroadcastsCommitOnNewQuorum(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	env.setLeader(1)

	fullData := []byte("proposal-value")
	root := env.hash(fullData)
	env.inst.State.ProposalAcceptedForCurrentRound = env.proposal(1, 1, fullData, root, nil, nil)

	env.addMessages(
		env.inst.State.PrepareContainer,
		env.prepare(1, 1, root),
		env.prepare(1, 3, root),
	)

	err := env.inst.uponPrepare(context.Background(), zap.NewNop(), env.prepare(1, 4, root))
	require.NoError(t, err)

	require.Equal(t, fullData, env.inst.State.LastPreparedValue)
	require.Equal(t, specqbft.Round(1), env.inst.State.LastPreparedRound)

	commit := env.broadcastedProcessingMessage(0)
	require.Equal(t, specqbft.CommitMsgType, commit.QBFTMessage.MsgType)
	require.Equal(t, specqbft.Round(1), commit.QBFTMessage.Round)
	require.Equal(t, root, commit.QBFTMessage.Root)
	require.Equal(t, []spectypes.OperatorID{2}, commit.SignedMessage.OperatorIDs)
}

func TestUponPrepareReturnsEarlyWhenQuorumAlreadyReached(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	env.setLeader(1)

	fullData := []byte("proposal-value")
	root := env.hash(fullData)
	env.inst.State.ProposalAcceptedForCurrentRound = env.proposal(1, 1, fullData, root, nil, nil)
	env.inst.State.LastPreparedValue = []byte("existing")
	env.inst.State.LastPreparedRound = 9

	env.addMessages(
		env.inst.State.PrepareContainer,
		env.prepare(1, 1, root),
		env.prepare(1, 3, root),
		env.prepare(1, 4, root),
	)

	err := env.inst.uponPrepare(context.Background(), zap.NewNop(), env.prepare(1, 2, root))
	require.NoError(t, err)

	require.Equal(t, []byte("existing"), env.inst.State.LastPreparedValue)
	require.Equal(t, specqbft.Round(9), env.inst.State.LastPreparedRound)
	require.Empty(t, env.network.BroadcastedMsgs)
}

func TestUponPrepareWithoutQuorumDoesNotBroadcast(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	env.setLeader(1)

	fullData := []byte("proposal-value")
	root := env.hash(fullData)
	env.inst.State.ProposalAcceptedForCurrentRound = env.proposal(1, 1, fullData, root, nil, nil)
	env.addMessages(env.inst.State.PrepareContainer, env.prepare(1, 1, root))

	err := env.inst.uponPrepare(context.Background(), zap.NewNop(), env.prepare(1, 3, root))
	require.NoError(t, err)

	require.Nil(t, env.inst.State.LastPreparedValue)
	require.Equal(t, specqbft.NoRound, env.inst.State.LastPreparedRound)
	require.Empty(t, env.network.BroadcastedMsgs)
}

func TestGetRoundChangeJustificationFiltersMatchingPrepares(t *testing.T) {
	env := newInstanceTestEnv(t, 2)

	fullData := []byte("proposal-value")
	root := env.hash(fullData)
	env.inst.State.LastPreparedValue = fullData
	env.inst.State.LastPreparedRound = 1

	env.addMessages(
		env.inst.State.PrepareContainer,
		env.prepare(1, 1, root),
		env.prepare(1, 2, root),
		env.prepare(1, 3, root),
		env.prepare(1, 4, [32]byte{7}),
	)

	justification, err := env.inst.getRoundChangeJustification()
	require.NoError(t, err)
	require.Len(t, justification, 3)
	for _, msg := range justification {
		require.Equal(t, root, msg.QBFTMessage.Root)
	}
}

func TestGetRoundChangeJustificationReturnsNilWhenNoPreparedValueOrNoQuorum(t *testing.T) {
	t.Run("no prepared value", func(t *testing.T) {
		env := newInstanceTestEnv(t, 2)
		justification, err := env.inst.getRoundChangeJustification()
		require.NoError(t, err)
		require.Nil(t, justification)
	})

	t.Run("filtered prepares do not reach quorum", func(t *testing.T) {
		env := newInstanceTestEnv(t, 2)
		fullData := []byte("proposal-value")
		root := env.hash(fullData)
		env.inst.State.LastPreparedValue = fullData
		env.inst.State.LastPreparedRound = 1
		env.addMessages(
			env.inst.State.PrepareContainer,
			env.prepare(1, 1, root),
			env.prepare(1, 2, [32]byte{5}),
			env.prepare(1, 3, [32]byte{6}),
		)

		justification, err := env.inst.getRoundChangeJustification()
		require.NoError(t, err)
		require.Nil(t, justification)
	})
}

func TestValidSignedPrepareForHeightRoundAndRootIgnoreSignature(t *testing.T) {
	env := newInstanceTestEnv(t, 2)

	root := env.hash([]byte("proposal-value"))

	t.Run("wrong type is rejected", func(t *testing.T) {
		err := env.inst.validSignedPrepareForHeightRoundAndRootIgnoreSignature(env.commit(1, 1, root), 1, root)
		require.ErrorContains(t, err, "prepare msg type is wrong")
	})

	t.Run("wrong height is rejected", func(t *testing.T) {
		msg := env.processingMessage(&specqbft.Message{
			MsgType:    specqbft.PrepareMsgType,
			Height:     env.inst.State.Height + 1,
			Round:      1,
			Identifier: env.inst.State.ID,
			Root:       root,
		}, 1, nil)
		err := env.inst.validSignedPrepareForHeightRoundAndRootIgnoreSignature(msg, 1, root)
		require.ErrorContains(t, err, ErrWrongMsgHeight.Error())
	})

	t.Run("wrong round is retryable", func(t *testing.T) {
		err := env.inst.validSignedPrepareForHeightRoundAndRootIgnoreSignature(env.prepare(2, 1, root), 1, root)
		require.Error(t, err)
		require.True(t, IsRetryable(err))
	})

	t.Run("wrong root is rejected", func(t *testing.T) {
		err := env.inst.validSignedPrepareForHeightRoundAndRootIgnoreSignature(env.prepare(1, 1, [32]byte{8}), 1, root)
		require.ErrorContains(t, err, "proposed data mismatch")
	})

	t.Run("multiple signers are rejected", func(t *testing.T) {
		msg := env.aggregateMessages(env.prepare(1, 1, root), env.prepare(1, 2, root))
		err := env.inst.validSignedPrepareForHeightRoundAndRootIgnoreSignature(msg, 1, root)
		require.ErrorContains(t, err, "signer not in committee")
	})

	t.Run("signer not in committee is rejected", func(t *testing.T) {
		msg := env.processingMessageWithKey(&specqbft.Message{
			MsgType:    specqbft.PrepareMsgType,
			Height:     env.inst.State.Height,
			Round:      1,
			Identifier: env.inst.State.ID,
			Root:       root,
		}, 99, env.keys.OperatorKeys[2], nil)
		err := env.inst.validSignedPrepareForHeightRoundAndRootIgnoreSignature(msg, 1, root)
		require.ErrorContains(t, err, "signer not in committee")
	})
}

func TestValidSignedPrepareForHeightRoundAndRootVerifySignatureRejectsInvalidSignature(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	root := env.hash([]byte("proposal-value"))

	msg := env.processingMessageWithKey(&specqbft.Message{
		MsgType:    specqbft.PrepareMsgType,
		Height:     env.inst.State.Height,
		Round:      1,
		Identifier: env.inst.State.ID,
		Root:       root,
	}, 1, env.keys.OperatorKeys[2], nil)

	err := env.inst.validSignedPrepareForHeightRoundAndRootVerifySignature(msg, 1, root)
	require.ErrorContains(t, err, "msg signature invalid")
}
