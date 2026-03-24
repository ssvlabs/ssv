package instance

import (
	"context"
	"testing"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestUponProposalFutureRoundBumpsAndBroadcastsPrepare(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	env.setLeader(1)

	fullData := []byte("proposal-value")
	proposal := env.proposal(2, 1, fullData, env.hash(fullData), nil, nil)

	err := env.inst.uponProposal(context.Background(), zap.NewNop(), proposal)
	require.NoError(t, err)

	require.Equal(t, specqbft.Round(2), env.inst.State.Round)
	require.Same(t, proposal, env.inst.State.ProposalAcceptedForCurrentRound)
	require.Equal(t, 1, env.timer.State.Timeouts)
	require.Equal(t, specqbft.Round(2), env.timer.State.Round)

	prepare := env.broadcastedProcessingMessage(0)
	require.Equal(t, specqbft.PrepareMsgType, prepare.QBFTMessage.MsgType)
	require.Equal(t, specqbft.Round(2), prepare.QBFTMessage.Round)
	require.Equal(t, env.hash(fullData), prepare.QBFTMessage.Root)
	require.Equal(t, []spectypes.OperatorID{2}, prepare.SignedMessage.OperatorIDs)
}

func TestUponProposalDuplicateIgnored(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	env.setLeader(1)

	fullData := []byte("proposal-value")
	proposal := env.proposal(1, 1, fullData, env.hash(fullData), nil, nil)

	require.NoError(t, env.inst.uponProposal(context.Background(), zap.NewNop(), proposal))
	require.NoError(t, env.inst.uponProposal(context.Background(), zap.NewNop(), proposal))

	require.Len(t, env.network.BroadcastedMsgs, 1)
}

func TestIsValidProposalRejectsWrongLeader(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	env.setLeader(1)

	fullData := []byte("proposal-value")
	proposal := env.proposal(1, 2, fullData, env.hash(fullData), nil, nil)

	err := env.inst.isValidProposal(proposal)
	require.ErrorContains(t, err, "proposal leader invalid")
}

func TestIsValidProposalValidationBranches(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	env.setLeader(1)
	fullData := []byte("proposal-value")
	root := env.hash(fullData)

	t.Run("wrong type", func(t *testing.T) {
		err := env.inst.isValidProposal(env.prepare(1, 1, root))
		require.ErrorContains(t, err, "msg type is not proposal")
	})

	t.Run("wrong height", func(t *testing.T) {
		msg := env.processingMessage(&specqbft.Message{
			MsgType:    specqbft.ProposalMsgType,
			Height:     env.inst.State.Height + 1,
			Round:      1,
			Identifier: env.inst.State.ID,
			Root:       root,
		}, 1, fullData)
		err := env.inst.isValidProposal(msg)
		require.ErrorContains(t, err, ErrWrongMsgHeight.Error())
	})

	t.Run("multiple signers", func(t *testing.T) {
		msg := env.aggregateMessages(
			env.proposal(1, 1, fullData, root, nil, nil),
			env.proposal(1, 2, fullData, root, nil, nil),
		)
		err := env.inst.isValidProposal(msg)
		require.ErrorContains(t, err, "msg allows 1 signer")
	})

	t.Run("signer not in committee", func(t *testing.T) {
		msg := env.processingMessageWithKey(&specqbft.Message{
			MsgType:    specqbft.ProposalMsgType,
			Height:     env.inst.State.Height,
			Round:      1,
			Identifier: env.inst.State.ID,
			Root:       root,
		}, 99, env.keys.OperatorKeys[2], fullData)
		err := env.inst.isValidProposal(msg)
		require.ErrorContains(t, err, "signer not in committee")
	})

	t.Run("current state rejects second proposal for same round", func(t *testing.T) {
		env.inst.State.Round = 1
		env.inst.State.ProposalAcceptedForCurrentRound = env.proposal(1, 1, fullData, root, nil, nil)
		err := env.inst.isValidProposal(env.proposal(1, 1, fullData, root, nil, nil))
		require.ErrorContains(t, err, "proposal is not valid with current state")
	})
}

func TestIsValidProposalRejectsRootMismatch(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	env.setLeader(1)

	fullData := []byte("proposal-value")
	proposal := env.proposal(1, 1, fullData, [32]byte{9, 9, 9}, nil, nil)

	err := env.inst.isValidProposal(proposal)
	require.ErrorContains(t, err, "H(data) != root")
}

func TestIsValidProposalRejectsInvalidValue(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	env.setLeader(1)

	err := env.inst.isValidProposal(env.proposal(1, 1, []byte("invalid-value"), env.hash([]byte("invalid-value")), nil, nil))
	require.ErrorContains(t, err, "proposal fullData invalid")
}

func TestIsProposalJustificationRequiresRoundChangeQuorum(t *testing.T) {
	env := newInstanceTestEnv(t, 2)

	fullData := []byte("proposal-value")
	roundChanges := []*specqbft.ProcessingMessage{
		env.roundChange(2, 1, specqbft.NoRound, [32]byte{}, nil, nil),
		env.roundChange(2, 2, specqbft.NoRound, [32]byte{}, nil, nil),
	}

	err := env.inst.isProposalJustification(roundChanges, nil, 2, fullData)
	require.ErrorContains(t, err, "change round has no quorum")
}

func TestIsProposalJustificationRejectsPreparedRoundChangeValueMismatch(t *testing.T) {
	env := newInstanceTestEnv(t, 2)

	preparedValue := []byte("prepared-value")
	roundChanges, prepares := env.preparedRoundChangeSet(2, 1, preparedValue, []spectypes.OperatorID{1, 2, 3}, []spectypes.OperatorID{1, 2, 3})

	err := env.inst.isProposalJustification(roundChanges, prepares, 2, []byte("different-value"))
	require.ErrorContains(t, err, "change round msg not valid")
}

func TestIsProposalJustificationRequiresPrepareQuorumForPreparedRoundChange(t *testing.T) {
	env := newInstanceTestEnv(t, 2)

	fullData := []byte("prepared-value")
	roundChanges, prepares := env.preparedRoundChangeSet(2, 1, fullData, []spectypes.OperatorID{1, 2, 3}, []spectypes.OperatorID{1, 2, 3})

	err := env.inst.isProposalJustification(roundChanges, prepares[:2], 2, fullData)
	require.ErrorContains(t, err, "prepares has no quorum")
}

func TestIsProposalJustificationRejectsInvalidPrepareJustification(t *testing.T) {
	env := newInstanceTestEnv(t, 2)

	fullData := []byte("prepared-value")
	root := env.hash(fullData)
	roundChanges, _ := env.preparedRoundChangeSet(2, 1, fullData, []spectypes.OperatorID{1, 2, 3}, []spectypes.OperatorID{1, 2, 3})
	prepares := []*specqbft.ProcessingMessage{
		env.processingMessageWithKey(&specqbft.Message{
			MsgType:    specqbft.PrepareMsgType,
			Height:     env.inst.State.Height,
			Round:      1,
			Identifier: env.inst.State.ID,
			Root:       root,
		}, 1, env.keys.OperatorKeys[2], nil),
		env.prepare(1, 2, root),
		env.prepare(1, 3, root),
	}

	err := env.inst.isProposalJustification(roundChanges, prepares, 2, fullData)
	require.ErrorContains(t, err, "signed prepare not valid")
}

func TestIsProposalJustificationAcceptsPreparedQuorum(t *testing.T) {
	env := newInstanceTestEnv(t, 2)

	fullData := []byte("prepared-value")
	roundChanges, prepares := env.preparedRoundChangeSet(2, 1, fullData, []spectypes.OperatorID{1, 2, 3}, []spectypes.OperatorID{1, 2, 3})

	err := env.inst.isProposalJustification(roundChanges, prepares, 2, fullData)
	require.NoError(t, err)
}
