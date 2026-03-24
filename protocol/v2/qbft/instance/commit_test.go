package instance

import (
	"context"
	"testing"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestAggregateCommitMsgsSortsOperatorIDsWithMatchingSignatures(t *testing.T) {
	env := newInstanceTestEnv(t, 4)

	fullData := []byte("commit-value")
	root := env.hash(fullData)
	msgs := []*specqbft.ProcessingMessage{
		env.commit(1, 3, root),
		env.commit(1, 1, root),
		env.commit(1, 2, root),
	}

	aggregated, err := aggregateCommitMsgs(msgs, fullData)
	require.NoError(t, err)

	require.Equal(t, []spectypes.OperatorID{1, 2, 3}, aggregated.OperatorIDs)
	require.Equal(t, fullData, aggregated.FullData)
	require.NoError(t, spectypes.Verify(aggregated, env.inst.State.CommitteeMember.Committee))
}

func TestAggregateCommitMsgsRejectsZeroMessages(t *testing.T) {
	aggregated, err := aggregateCommitMsgs(nil, []byte("commit-value"))
	require.Nil(t, aggregated)
	require.ErrorContains(t, err, "can't aggregate zero commit msgs")
}

func TestUponCommitReturnsDecidedOnQuorum(t *testing.T) {
	env := newInstanceTestEnv(t, 4)
	env.setLeader(1)

	fullData := []byte("commit-value")
	root := env.hash(fullData)
	env.inst.State.ProposalAcceptedForCurrentRound = env.proposal(1, 1, fullData, root, nil, nil)

	env.addMessages(
		env.inst.State.CommitContainer,
		env.commit(1, 1, root),
		env.commit(1, 2, root),
	)

	decided, decidedValue, aggregated, err := env.inst.UponCommit(
		context.Background(),
		zap.NewNop(),
		env.commit(1, 3, root),
	)
	require.NoError(t, err)
	require.True(t, decided)
	require.Equal(t, fullData, decidedValue)
	require.NotNil(t, aggregated)
	require.NoError(t, spectypes.Verify(aggregated, env.inst.State.CommitteeMember.Committee))
}

func TestUponCommitNoQuorumOrDuplicateReturnsNoDecision(t *testing.T) {
	env := newInstanceTestEnv(t, 4)
	env.setLeader(1)

	fullData := []byte("commit-value")
	root := env.hash(fullData)
	msg := env.commit(1, 1, root)
	env.inst.State.ProposalAcceptedForCurrentRound = env.proposal(1, 1, fullData, root, nil, nil)

	decided, decidedValue, aggregated, err := env.inst.UponCommit(context.Background(), zap.NewNop(), msg)
	require.NoError(t, err)
	require.False(t, decided)
	require.Nil(t, decidedValue)
	require.Nil(t, aggregated)

	decided, decidedValue, aggregated, err = env.inst.UponCommit(context.Background(), zap.NewNop(), msg)
	require.NoError(t, err)
	require.False(t, decided)
	require.Nil(t, decidedValue)
	require.Nil(t, aggregated)
}

func TestBaseCommitValidation(t *testing.T) {
	env := newInstanceTestEnv(t, 4)
	root := env.hash([]byte("commit-value"))

	t.Run("wrong type", func(t *testing.T) {
		err := baseCommitValidationIgnoreSignature(env.prepare(1, 1, root), env.inst.State.Height, env.inst.State.CommitteeMember.Committee)
		require.ErrorContains(t, err, "commit msg type is wrong")
	})

	t.Run("wrong height", func(t *testing.T) {
		msg := env.processingMessage(&specqbft.Message{
			MsgType:    specqbft.CommitMsgType,
			Height:     env.inst.State.Height + 1,
			Round:      1,
			Identifier: env.inst.State.ID,
			Root:       root,
		}, 1, nil)
		err := baseCommitValidationIgnoreSignature(msg, env.inst.State.Height, env.inst.State.CommitteeMember.Committee)
		require.ErrorContains(t, err, ErrWrongMsgHeight.Error())
	})

	t.Run("signer not in committee", func(t *testing.T) {
		msg := env.processingMessageWithKey(&specqbft.Message{
			MsgType:    specqbft.CommitMsgType,
			Height:     env.inst.State.Height,
			Round:      1,
			Identifier: env.inst.State.ID,
			Root:       root,
		}, 99, env.keys.OperatorKeys[2], nil)
		err := baseCommitValidationIgnoreSignature(msg, env.inst.State.Height, env.inst.State.CommitteeMember.Committee)
		require.ErrorContains(t, err, "signer not in committee")
	})

	t.Run("invalid signature", func(t *testing.T) {
		msg := env.processingMessageWithKey(&specqbft.Message{
			MsgType:    specqbft.CommitMsgType,
			Height:     env.inst.State.Height,
			Round:      1,
			Identifier: env.inst.State.ID,
			Root:       root,
		}, 1, env.keys.OperatorKeys[2], nil)
		err := BaseCommitValidationVerifySignature(msg, env.inst.State.Height, env.inst.State.CommitteeMember.Committee)
		require.ErrorContains(t, err, "msg signature invalid")
	})
}

func TestValidateCommit(t *testing.T) {
	env := newInstanceTestEnv(t, 4)
	env.setLeader(1)

	fullData := []byte("commit-value")
	root := env.hash(fullData)
	env.inst.State.Round = 2
	env.inst.State.ProposalAcceptedForCurrentRound = env.proposal(2, 1, fullData, root, nil, nil)

	t.Run("wrong round is retryable", func(t *testing.T) {
		err := env.inst.validateCommit(env.commit(1, 1, root))
		require.Error(t, err)
		require.True(t, IsRetryable(err))
	})

	t.Run("mismatched root is rejected", func(t *testing.T) {
		err := env.inst.validateCommit(env.commit(2, 1, [32]byte{6}))
		require.ErrorContains(t, err, "proposed data mismatch")
	})

	t.Run("multiple signers are rejected", func(t *testing.T) {
		msg := env.aggregateMessages(env.commit(2, 1, root), env.commit(2, 2, root))
		err := env.inst.validateCommit(msg)
		require.ErrorContains(t, err, "msg allows 1 signer")
	})
}
