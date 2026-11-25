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

	decided, decidedValue, aggregated, err := env.inst.uponCommit(
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

	decided, decidedValue, aggregated, err := env.inst.uponCommit(context.Background(), zap.NewNop(), msg)
	require.NoError(t, err)
	require.False(t, decided)
	require.Nil(t, decidedValue)
	require.Nil(t, aggregated)

	decided, decidedValue, aggregated, err = env.inst.uponCommit(context.Background(), zap.NewNop(), msg)
	require.NoError(t, err)
	require.False(t, decided)
	require.Nil(t, decidedValue)
	require.Nil(t, aggregated)
}

func TestUponCommitPostDecisionRecordsLateCommitWithoutRedeciding(t *testing.T) {
	env := newInstanceTestEnv(t, 4)
	env.setLeader(1)

	fullData := []byte("commit-value")
	root := env.hash(fullData)
	env.inst.State.ProposalAcceptedForCurrentRound = env.proposal(1, 1, fullData, root, nil, nil)
	env.inst.State.Decided = true
	env.inst.State.DecidedValue = fullData
	env.addMessages(
		env.inst.State.CommitContainer,
		env.commit(1, 1, root),
		env.commit(1, 2, root),
		env.commit(1, 3, root),
	)

	lateCommit := env.commit(1, 4, root)
	decided, decidedValue, aggregated, err := env.inst.uponCommit(context.Background(), zap.NewNop(), lateCommit)
	require.NoError(t, err)
	require.False(t, decided)
	require.Nil(t, decidedValue)
	require.Nil(t, aggregated)

	signers, msgs := env.inst.State.CommitContainer.LongestUniqueSignersForRoundAndRoot(1, root)
	require.Len(t, signers, 4)
	require.Len(t, msgs, 4)
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

// TestNonCanonicalCommitIsRejected checks that a validly signed commit that is not byte-identical
// to what CreateCommit produces never reaches the commit container: aggregation needs
// byte-identical SSVMessages. The instance still decides on the quorum of canonical commits.
func TestNonCanonicalCommitIsRejected(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(env *instanceTestEnv, qbftMsg *specqbft.Message, ssvMsg *spectypes.SSVMessage)
		wantErr string
	}{
		{"non-zero data round", func(_ *instanceTestEnv, m *specqbft.Message, _ *spectypes.SSVMessage) {
			m.DataRound = 1
		}, "commit is not canonical"},
		{"prepare justification", func(env *instanceTestEnv, m *specqbft.Message, _ *spectypes.SSVMessage) {
			m.PrepareJustification = env.marshalJustifications([]*specqbft.ProcessingMessage{env.prepare(1, 2, m.Root)})
		}, "unexpected prepare justification"},
		{"round-change justification", func(env *instanceTestEnv, m *specqbft.Message, _ *spectypes.SSVMessage) {
			m.RoundChangeJustification = env.marshalJustifications([]*specqbft.ProcessingMessage{env.prepare(1, 2, m.Root)})
		}, "unexpected round change justification"},
		{"outer msg id domain byte", func(_ *instanceTestEnv, _ *specqbft.Message, s *spectypes.SSVMessage) {
			s.MsgID[0] ^= 0xff
		}, "commit is not canonical"},
		{"outer msg id padding byte", func(_ *instanceTestEnv, _ *specqbft.Message, s *spectypes.SSVMessage) {
			s.MsgID[20] = 0xff
		}, "commit is not canonical"},
		// Defense in depth: routing never hands a non-consensus message to the QBFT instance, so this
		// shape cannot arrive in practice.
		{"outer msg type", func(_ *instanceTestEnv, _ *specqbft.Message, s *spectypes.SSVMessage) {
			s.MsgType = spectypes.SSVPartialSignatureMsgType
		}, "commit is not canonical"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newInstanceTestEnv(t, 1)
			env.setLeader(1)

			fullData := []byte("commit-value")
			root := env.hash(fullData)
			env.inst.State.ProposalAcceptedForCurrentRound = env.proposal(1, 1, fullData, root, nil, nil)

			qbftMsg := &specqbft.Message{
				MsgType:    specqbft.CommitMsgType,
				Height:     env.inst.State.Height,
				Round:      1,
				Identifier: env.inst.State.ID,
				Root:       root,
			}
			ssvMsg := &spectypes.SSVMessage{MsgType: spectypes.SSVConsensusMsgType}
			copy(ssvMsg.MsgID[:], env.inst.State.ID)
			tt.mutate(env, qbftMsg, ssvMsg)

			data, err := qbftMsg.Encode()
			require.NoError(t, err)
			ssvMsg.Data = data
			sig, err := spectypes.SignSSVMessage(env.keys.OperatorKeys[4], ssvMsg)
			require.NoError(t, err)
			nonCanonical, err := specqbft.NewProcessingMessage(&spectypes.SignedSSVMessage{
				OperatorIDs: []spectypes.OperatorID{4},
				Signatures:  [][]byte{sig},
				SSVMessage:  ssvMsg,
			})
			require.NoError(t, err)

			// The non-canonical commit arrives first, before the quorum.
			_, _, _, nonCanonicalErr := env.inst.ProcessMsg(context.Background(), zap.NewNop(), nonCanonical)

			for _, signer := range []spectypes.OperatorID{1, 2, 3} {
				_, _, _, err := env.inst.ProcessMsg(context.Background(), zap.NewNop(), env.commit(1, signer, root))
				require.NoError(t, err)
			}

			decided, value := env.inst.IsDecided()
			require.True(t, decided, "the quorum of canonical commits must still decide")
			require.Equal(t, fullData, value)
			require.ErrorContains(t, nonCanonicalErr, tt.wantErr)
		})
	}
}
