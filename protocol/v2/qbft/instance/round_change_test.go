package instance

import (
	"context"
	"testing"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHasReceivedPartialQuorumIgnoresCurrentAndPastRounds(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	env.inst.State.Round = 2

	env.addMessages(
		env.inst.State.RoundChangeContainer,
		env.roundChange(1, 1, specqbft.NoRound, [32]byte{}, nil, nil),
		env.roundChange(2, 2, specqbft.NoRound, [32]byte{}, nil, nil),
		env.roundChange(3, 3, specqbft.NoRound, [32]byte{}, nil, nil),
		env.roundChange(3, 4, specqbft.NoRound, [32]byte{}, nil, nil),
	)

	partialQuorum, msgs := env.inst.hasReceivedPartialQuorum()
	require.True(t, partialQuorum)
	require.Len(t, msgs, 2)
	for _, msg := range msgs {
		require.Greater(t, msg.QBFTMessage.Round, specqbft.Round(2))
	}
}

func TestHasReceivedPartialQuorumRequiresPartialThreshold(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	env.inst.State.Round = 2
	env.addMessages(env.inst.State.RoundChangeContainer, env.roundChange(3, 3, specqbft.NoRound, [32]byte{}, nil, nil))

	partialQuorum, msgs := env.inst.hasReceivedPartialQuorum()
	require.False(t, partialQuorum)
	require.Len(t, msgs, 1)
}

func TestHighestPreparedReturnsRoundChangeWithHighestPreparedRound(t *testing.T) {
	env := newInstanceTestEnv(t, 2)

	fullData := []byte("prepared-value")
	root := env.hash(fullData)
	highest, err := highestPrepared([]*specqbft.ProcessingMessage{
		env.roundChange(2, 1, specqbft.NoRound, [32]byte{}, nil, nil),
		env.roundChange(2, 2, 1, root, fullData, []*specqbft.ProcessingMessage{
			env.prepare(1, 1, root),
			env.prepare(1, 2, root),
			env.prepare(1, 3, root),
		}),
		env.roundChange(2, 3, 2, root, fullData, []*specqbft.ProcessingMessage{
			env.prepare(2, 1, root),
			env.prepare(2, 2, root),
			env.prepare(2, 3, root),
		}),
	})
	require.NoError(t, err)
	require.NotNil(t, highest)
	require.Equal(t, specqbft.Round(2), highest.QBFTMessage.DataRound)
	require.Equal(t, []spectypes.OperatorID{3}, highest.SignedMessage.OperatorIDs)
}

func TestHasReceivedProposalJustificationForLeadingRound(t *testing.T) {
	t.Run("returns nil without quorum", func(t *testing.T) {
		env := newInstanceTestEnv(t, 1)
		env.setLeader(1)
		msg := env.roundChange(2, 2, specqbft.NoRound, [32]byte{}, nil, nil)
		env.addMessages(env.inst.State.RoundChangeContainer, msg)

		got, value, err := env.inst.hasReceivedProposalJustificationForLeadingRound(msg)
		require.NoError(t, err)
		require.Nil(t, got)
		require.Nil(t, value)
	})

	t.Run("returns highest prepared value for leader", func(t *testing.T) {
		env := newInstanceTestEnv(t, 1)
		env.setLeader(1)
		env.inst.State.Round = 2
		env.inst.StartValue = []byte("start-value")

		value := []byte("prepared-b")
		root := env.hash(value)

		rc1 := env.roundChange(2, 2, specqbft.NoRound, [32]byte{}, nil, nil)
		rc2 := env.roundChange(2, 3, 1, root, value, []*specqbft.ProcessingMessage{
			env.prepare(1, 1, root),
			env.prepare(1, 2, root),
			env.prepare(1, 3, root),
		})
		rc3 := env.roundChange(2, 4, 2, root, value, []*specqbft.ProcessingMessage{
			env.prepare(2, 1, root),
			env.prepare(2, 2, root),
			env.prepare(2, 4, root),
		})
		env.addMessages(env.inst.State.RoundChangeContainer, rc1, rc2, rc3)

		got, value, err := env.inst.hasReceivedProposalJustificationForLeadingRound(rc1)
		require.NoError(t, err)
		require.Same(t, rc3, got)
		require.Equal(t, []byte("prepared-b"), value)
	})
}

func TestIsProposalJustificationForLeadingRound(t *testing.T) {
	newLeadingRoundEnv := func(t *testing.T) (*instanceTestEnv, []byte, []*specqbft.ProcessingMessage, *specqbft.ProcessingMessage) {
		t.Helper()

		env := newInstanceTestEnv(t, 1)
		env.setLeader(1)
		env.inst.State.Round = 2
		value := []byte("proposal-value")
		roundChanges := []*specqbft.ProcessingMessage{
			env.roundChange(2, 2, specqbft.NoRound, [32]byte{}, nil, nil),
			env.roundChange(2, 3, specqbft.NoRound, [32]byte{}, nil, nil),
			env.roundChange(2, 4, specqbft.NoRound, [32]byte{}, nil, nil),
		}
		return env, value, roundChanges, roundChanges[0]
	}

	t.Run("not proposer", func(t *testing.T) {
		nonLeaderEnv := newInstanceTestEnv(t, 2)
		nonLeaderEnv.setLeader(1)
		nonLeaderEnv.inst.State.Round = 2
		_, value, roundChanges, roundChangeMsg := newLeadingRoundEnv(t)
		err := nonLeaderEnv.inst.isProposalJustificationForLeadingRound(roundChangeMsg, roundChanges, nil, value, 2)
		require.ErrorContains(t, err, "not proposer")
	})

	t.Run("proposal round mismatch", func(t *testing.T) {
		env, value, roundChanges, roundChangeMsg := newLeadingRoundEnv(t)
		env.inst.State.ProposalAcceptedForCurrentRound = env.proposal(2, 1, value, env.hash(value), nil, nil)
		err := env.inst.isProposalJustificationForLeadingRound(roundChangeMsg, roundChanges, nil, value, 2)
		require.ErrorContains(t, err, "proposal round mismatch")
	})

	t.Run("accepts future round proposal", func(t *testing.T) {
		env, value, roundChanges, roundChangeMsg := newLeadingRoundEnv(t)
		env.inst.State.ProposalAcceptedForCurrentRound = nil
		err := env.inst.isProposalJustificationForLeadingRound(roundChangeMsg, roundChanges, nil, value, 3)
		require.NoError(t, err)
	})
}

func TestUponRoundChangeWithPartialQuorumBumpsRoundAndBroadcastsRoundChange(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	env.setLeader(1)
	env.inst.State.Round = 1
	env.inst.State.ProposalAcceptedForCurrentRound = env.proposal(1, 1, []byte("old-value"), env.hash([]byte("old-value")), nil, nil)

	env.addMessages(
		env.inst.State.RoundChangeContainer,
		env.roundChange(2, 1, specqbft.NoRound, [32]byte{}, nil, nil),
	)

	err := env.inst.uponRoundChange(
		context.Background(),
		zap.NewNop(),
		env.roundChange(2, 3, specqbft.NoRound, [32]byte{}, nil, nil),
	)
	require.NoError(t, err)

	require.Equal(t, specqbft.Round(2), env.inst.State.Round)
	require.Nil(t, env.inst.State.ProposalAcceptedForCurrentRound)
	require.Equal(t, 1, env.roundTimer.State.Timeouts)
	require.Equal(t, specqbft.Round(2), env.roundTimer.State.Round)

	msg := env.broadcastedProcessingMessage(0)
	require.Equal(t, specqbft.RoundChangeMsgType, msg.QBFTMessage.MsgType)
	require.Equal(t, specqbft.Round(2), msg.QBFTMessage.Round)
	require.Equal(t, specqbft.NoRound, msg.QBFTMessage.DataRound)
	require.Equal(t, []spectypes.OperatorID{2}, msg.SignedMessage.OperatorIDs)
}

func TestUponChangeRoundPartialQuorumGivesUpQuietlyAtCutOffRound(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	// A role whose cap equals CutOffRound validates round-change messages at the cap but gives up there.
	// A partial-quorum jump to the cap bumps the instance into the cutoff, so it gives up quietly, no error.
	env.config.CutOffRound = env.inst.State.Round + 1
	cutOffRound := env.config.CutOffRound

	err := env.inst.uponChangeRoundPartialQuorum(context.Background(), zap.NewNop(), cutOffRound)
	require.NoError(t, err)
	require.Equal(t, cutOffRound, env.inst.State.Round)
	require.Zero(t, env.roundTimer.State.Timeouts, "no timer armed once the instance gives up at the cutoff")
	require.Empty(t, env.network.BroadcastedMsgs, "no round-change broadcast at the cutoff")
}

func TestUponRoundChangeReturnsEarlyOnDuplicate(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	msg := env.roundChange(2, 1, specqbft.NoRound, [32]byte{}, nil, nil)
	env.addMessages(env.inst.State.RoundChangeContainer, msg)

	currentRound := env.inst.State.Round
	require.NoError(t, env.inst.uponRoundChange(context.Background(), zap.NewNop(), msg))
	require.Empty(t, env.network.BroadcastedMsgs)
	require.Equal(t, currentRound, env.inst.State.Round)
}

func TestUponRoundChangeReturnsEarlyWhenQuorumAlreadyExists(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	env.addMessages(
		env.inst.State.RoundChangeContainer,
		env.roundChange(2, 1, specqbft.NoRound, [32]byte{}, nil, nil),
		env.roundChange(2, 3, specqbft.NoRound, [32]byte{}, nil, nil),
		env.roundChange(2, 4, specqbft.NoRound, [32]byte{}, nil, nil),
	)
	currentRound := env.inst.State.Round
	err := env.inst.uponRoundChange(context.Background(), zap.NewNop(), env.roundChange(2, 2, specqbft.NoRound, [32]byte{}, nil, nil))
	require.NoError(t, err)
	require.Empty(t, env.network.BroadcastedMsgs)
	require.Equal(t, currentRound, env.inst.State.Round)
}

func TestUponRoundChangeAsLeaderBroadcastsProposalOnJustifiedQuorum(t *testing.T) {
	env := newInstanceTestEnv(t, 1)
	env.setLeader(1)
	env.inst.State.Round = 2
	env.inst.StartValue = []byte("start-value")

	env.addMessages(
		env.inst.State.RoundChangeContainer,
		env.roundChange(2, 2, specqbft.NoRound, [32]byte{}, nil, nil),
		env.roundChange(2, 3, specqbft.NoRound, [32]byte{}, nil, nil),
	)

	err := env.inst.uponRoundChange(
		context.Background(),
		zap.NewNop(),
		env.roundChange(2, 4, specqbft.NoRound, [32]byte{}, nil, nil),
	)
	require.NoError(t, err)

	msg := env.broadcastedProcessingMessage(0)
	require.Equal(t, specqbft.ProposalMsgType, msg.QBFTMessage.MsgType)
	require.Equal(t, specqbft.Round(2), msg.QBFTMessage.Round)
	require.Equal(t, env.hash(env.inst.StartValue), msg.QBFTMessage.Root)
}

func TestUponRoundChangeAsLeaderBroadcastsFutureRoundProposalOnJustifiedQuorum(t *testing.T) {
	env := newInstanceTestEnv(t, 1)
	env.setLeader(1)
	env.inst.State.Round = 1
	env.inst.StartValue = []byte("start-value")

	// Preload container to exercise the future-round leader path; this path is unreachable via
	// natural message flow (partial-quorum f+1 fires before full quorum 2f+1 in uponRoundChange),
	// kept as defense-in-depth against future refactors.
	env.addMessages(
		env.inst.State.RoundChangeContainer,
		env.roundChange(2, 2, specqbft.NoRound, [32]byte{}, nil, nil),
		env.roundChange(2, 3, specqbft.NoRound, [32]byte{}, nil, nil),
	)

	err := env.inst.uponRoundChange(
		context.Background(),
		zap.NewNop(),
		env.roundChange(2, 4, specqbft.NoRound, [32]byte{}, nil, nil),
	)
	require.NoError(t, err)

	require.Equal(t, specqbft.Round(2), env.inst.State.Round)

	msg := env.broadcastedProcessingMessage(0)
	require.Equal(t, specqbft.ProposalMsgType, msg.QBFTMessage.MsgType)
	require.Equal(t, specqbft.Round(2), msg.QBFTMessage.Round)
	require.Equal(t, env.hash(env.inst.StartValue), msg.QBFTMessage.Root)

	roundChangeJustifications, err := msg.QBFTMessage.GetRoundChangeJustifications()
	require.NoError(t, err)
	require.Len(t, roundChangeJustifications, 3)
}

func TestValidRoundChangeForDataIgnoreSignatureValidationBranches(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	fullData := []byte("prepared-value")
	root := env.hash(fullData)

	t.Run("wrong type", func(t *testing.T) {
		err := env.inst.validRoundChangeForDataIgnoreSignature(env.prepare(2, 1, root), 2, fullData)
		require.ErrorContains(t, err, "round change msg type is wrong")
	})

	t.Run("wrong height", func(t *testing.T) {
		msg := env.processingMessage(&specqbft.Message{
			MsgType:    specqbft.RoundChangeMsgType,
			Height:     env.inst.State.Height + 1,
			Round:      2,
			Identifier: env.inst.State.ID,
		}, 1, nil)
		err := env.inst.validRoundChangeForDataIgnoreSignature(msg, 2, fullData)
		require.ErrorContains(t, err, ErrWrongMsgHeight.Error())
	})

	t.Run("wrong round is retryable", func(t *testing.T) {
		err := env.inst.validRoundChangeForDataIgnoreSignature(env.roundChange(3, 1, specqbft.NoRound, [32]byte{}, nil, nil), 2, fullData)
		require.Error(t, err)
		require.True(t, IsRetryable(err))
	})

	t.Run("multiple signers", func(t *testing.T) {
		msg := env.aggregateMessages(
			env.roundChange(2, 1, specqbft.NoRound, [32]byte{}, nil, nil),
			env.roundChange(2, 2, specqbft.NoRound, [32]byte{}, nil, nil),
		)
		err := env.inst.validRoundChangeForDataIgnoreSignature(msg, 2, fullData)
		require.ErrorContains(t, err, "msg allows 1 signer")
	})

	t.Run("signer not in committee", func(t *testing.T) {
		msg := env.processingMessageWithKey(&specqbft.Message{
			MsgType:    specqbft.RoundChangeMsgType,
			Height:     env.inst.State.Height,
			Round:      2,
			Identifier: env.inst.State.ID,
		}, 99, env.keys.OperatorKeys[2], nil)
		err := env.inst.validRoundChangeForDataIgnoreSignature(msg, 2, fullData)
		require.ErrorContains(t, err, "signer not in committee")
	})

	t.Run("prepared full data root mismatch", func(t *testing.T) {
		msg := env.roundChange(2, 1, 1, [32]byte{9}, fullData, []*specqbft.ProcessingMessage{
			env.prepare(1, 1, [32]byte{9}),
			env.prepare(1, 2, [32]byte{9}),
			env.prepare(1, 3, [32]byte{9}),
		})
		err := env.inst.validRoundChangeForDataIgnoreSignature(msg, 2, fullData)
		require.ErrorContains(t, err, "H(data) != root")
	})

	t.Run("invalid prepare signature", func(t *testing.T) {
		msg := env.roundChange(2, 1, 1, root, fullData, []*specqbft.ProcessingMessage{
			env.processingMessageWithKey(&specqbft.Message{
				MsgType:    specqbft.PrepareMsgType,
				Height:     env.inst.State.Height,
				Round:      1,
				Identifier: env.inst.State.ID,
				Root:       root,
			}, 1, env.keys.OperatorKeys[2], nil),
			env.prepare(1, 2, root),
			env.prepare(1, 3, root),
		})
		err := env.inst.validRoundChangeForDataIgnoreSignature(msg, 2, fullData)
		require.ErrorContains(t, err, "round change justification invalid")
	})

	t.Run("unprepared with full data", func(t *testing.T) {
		// The shared check follows the spec (it also validates round changes embedded in a
		// proposal); only a round change received on its own refuses full data.
		msg := env.roundChange(2, 1, specqbft.NoRound, root, fullData, nil)
		require.NoError(t, env.inst.validRoundChangeForDataIgnoreSignature(msg, 2, nil))
		require.ErrorContains(t, env.inst.BaseMsgValidation(msg), "unprepared round change with full data")
	})
}

func TestUnpreparedRoundChangeWithFullDataIsNotStored(t *testing.T) {
	t.Run("root matches full data", func(t *testing.T) {
		env := newInstanceTestEnv(t, 1)
		env.inst.Start(t.Context(), []byte("start-value"), testValueChecker{})

		fullData := make([]byte, 1<<20)
		root := env.hash(fullData)

		for _, round := range []specqbft.Round{1, 2} {
			_, _, _, err := env.inst.ProcessMsg(t.Context(), zap.NewNop(), env.roundChange(round, 2, specqbft.NoRound, root, fullData, nil))
			require.ErrorContains(t, err, "unprepared round change with full data")
		}
		require.Empty(t, env.inst.State.RoundChangeContainer.AllMessages())
	})

	t.Run("root unrelated to full data", func(t *testing.T) {
		env := newInstanceTestEnv(t, 1)
		env.inst.Start(t.Context(), []byte("start-value"), testValueChecker{})

		_, _, _, err := env.inst.ProcessMsg(t.Context(), zap.NewNop(), env.roundChange(2, 2, specqbft.NoRound, [32]byte{0xAA}, []byte("x"), nil))
		require.ErrorContains(t, err, "unprepared round change with full data")
		require.Empty(t, env.inst.State.RoundChangeContainer.AllMessages())
	})
}

func TestWellFormedRoundChangesAccepted(t *testing.T) {
	newEnv := func(t *testing.T) *instanceTestEnv {
		env := newInstanceTestEnv(t, 1)
		env.inst.Start(t.Context(), []byte("start-value"), testValueChecker{})
		require.Equal(t, specqbft.FirstRound, env.inst.State.Round)
		return env
	}

	t.Run("unprepared with zero root", func(t *testing.T) {
		env := newEnv(t)
		_, _, _, err := env.inst.ProcessMsg(t.Context(), zap.NewNop(), env.roundChange(2, 2, specqbft.NoRound, [32]byte{}, nil, nil))
		require.NoError(t, err)
		require.Len(t, env.inst.State.RoundChangeContainer.MessagesForRound(2), 1)
	})

	t.Run("unprepared with non-zero root", func(t *testing.T) {
		// No rule on Root: it is signed, and with no full data there is nothing to bind it to.
		env := newEnv(t)
		_, _, _, err := env.inst.ProcessMsg(t.Context(), zap.NewNop(), env.roundChange(2, 2, specqbft.NoRound, [32]byte{1}, nil, nil))
		require.NoError(t, err)
		require.Len(t, env.inst.State.RoundChangeContainer.MessagesForRound(2), 1)
	})

	t.Run("prepared with full data", func(t *testing.T) {
		env := newEnv(t)
		value := []byte("prepared-value")
		root := env.hash(value)
		msg := env.roundChange(2, 2, 1, root, value, []*specqbft.ProcessingMessage{
			env.prepare(1, 1, root),
			env.prepare(1, 2, root),
			env.prepare(1, 3, root),
		})

		_, _, _, err := env.inst.ProcessMsg(t.Context(), zap.NewNop(), msg)
		require.NoError(t, err)

		stored := env.inst.State.RoundChangeContainer.MessagesForRound(2)
		require.Len(t, stored, 1)
		require.Equal(t, value, stored[0].SignedMessage.FullData)
	})
}

func TestProposalWithUnpreparedRoundChangeJustificationsStillValid(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	env.setLeader(1)
	env.inst.State.Round = 2

	value := []byte("proposal-value")
	roundChanges := []*specqbft.ProcessingMessage{
		env.roundChange(2, 1, specqbft.NoRound, [32]byte{}, nil, nil),
		env.roundChange(2, 2, specqbft.NoRound, [32]byte{}, nil, nil),
		env.roundChange(2, 3, specqbft.NoRound, [32]byte{}, nil, nil),
	}
	proposal := env.proposal(2, 1, value, env.hash(value), roundChanges, nil)

	require.NoError(t, env.inst.BaseMsgValidation(proposal))
}

// The full-data rule applies only to round changes received on their own, so a proposal embedding an
// unprepared round change that carries FullData stays valid. When the spec pin
// includes ssv-spec#673, Message.Validate refuses such an embedded justification with the spec's
// JustificationWithFullDataErrorCode, so the GetRoundChangeJustifications pre-check below fails first
// and this test should expect that error instead.
// The unprepared round change full-data check runs only on round changes received on their own,
// but a proposal that embeds one with full data is still refused: validateMsg refuses full data in
// any justification. Leaders never build one, since MarshalJustifications strips full data.
func TestProposalWithUnpreparedRoundChangeJustificationWithFullDataRejected(t *testing.T) {
	env := newInstanceTestEnv(t, 2)
	env.setLeader(1)
	env.inst.State.Round = 2

	value := []byte("proposal-value")
	roundChanges := []*specqbft.ProcessingMessage{
		env.roundChange(2, 1, specqbft.NoRound, [32]byte{}, []byte("junk"), nil),
		env.roundChange(2, 2, specqbft.NoRound, [32]byte{}, nil, nil),
		env.roundChange(2, 3, specqbft.NoRound, [32]byte{}, nil, nil),
	}

	// Marshal the round changes directly: MarshalJustifications would strip their FullData.
	justifications := make([][]byte, 0, len(roundChanges))
	for _, rc := range roundChanges {
		b, err := rc.SignedMessage.MarshalSSZ()
		require.NoError(t, err)
		justifications = append(justifications, b)
	}

	proposal := env.processingMessage(&specqbft.Message{
		MsgType:                  specqbft.ProposalMsgType,
		Height:                   env.inst.State.Height,
		Round:                    2,
		Identifier:               env.inst.State.ID,
		Root:                     env.hash(value),
		RoundChangeJustification: justifications,
	}, 1, value)

	embedded, err := proposal.QBFTMessage.GetRoundChangeJustifications()
	require.NoError(t, err)
	require.Equal(t, []byte("junk"), embedded[0].FullData)

	require.ErrorContains(t, env.inst.BaseMsgValidation(proposal), "justification with full data")
}

func TestValidRoundChangeForDataIgnoreSignatureRejectsInvalidPreparedJustification(t *testing.T) {
	env := newInstanceTestEnv(t, 2)

	fullData := []byte("prepared-value")
	root := env.hash(fullData)

	err := env.inst.validRoundChangeForDataIgnoreSignature(
		env.roundChange(2, 1, 1, root, fullData, []*specqbft.ProcessingMessage{
			env.prepare(1, 1, root),
			env.prepare(1, 2, root),
		}),
		2,
		fullData,
	)
	require.ErrorContains(t, err, "no justifications quorum")

	err = env.inst.validRoundChangeForDataIgnoreSignature(
		env.roundChange(2, 1, 3, root, fullData, []*specqbft.ProcessingMessage{
			env.prepare(3, 1, root),
			env.prepare(3, 2, root),
			env.prepare(3, 3, root),
		}),
		2,
		fullData,
	)
	require.ErrorContains(t, err, "prepared round > round")
}

func TestValidRoundChangeForDataVerifySignatureRejectsInvalidSignature(t *testing.T) {
	env := newInstanceTestEnv(t, 2)

	msg := env.processingMessageWithKey(&specqbft.Message{
		MsgType:    specqbft.RoundChangeMsgType,
		Height:     env.inst.State.Height,
		Round:      2,
		Identifier: env.inst.State.ID,
	}, 1, env.keys.OperatorKeys[2], nil)

	err := env.inst.validRoundChangeForDataVerifySignature(msg, 2, nil)
	require.ErrorContains(t, err, "msg signature invalid")
}

func TestGetRoundChangeDataAndCreateRoundChange(t *testing.T) {
	t.Run("returns empty data without prepared state", func(t *testing.T) {
		env := newInstanceTestEnv(t, 2)
		round, root, fullData, justifications, err := env.inst.getRoundChangeData()
		require.NoError(t, err)
		require.Equal(t, specqbft.NoRound, round)
		require.Equal(t, [32]byte{}, root)
		require.Nil(t, fullData)
		require.Nil(t, justifications)
	})

	t.Run("includes prepared round root value and justifications", func(t *testing.T) {
		env := newInstanceTestEnv(t, 2)
		fullData := []byte("prepared-value")
		root := env.hash(fullData)
		env.inst.State.LastPreparedRound = 1
		env.inst.State.LastPreparedValue = fullData
		env.addMessages(
			env.inst.State.PrepareContainer,
			env.prepare(1, 1, root),
			env.prepare(1, 2, root),
			env.prepare(1, 3, root),
		)

		round, gotRoot, gotValue, justifications, err := env.inst.getRoundChangeData()
		require.NoError(t, err)
		require.Equal(t, specqbft.Round(1), round)
		require.Equal(t, root, gotRoot)
		require.Equal(t, fullData, gotValue)
		require.Len(t, justifications, 3)

		signed, err := env.inst.CreateRoundChange(2)
		require.NoError(t, err)

		msg, err := specqbft.NewProcessingMessage(signed)
		require.NoError(t, err)
		require.Equal(t, specqbft.RoundChangeMsgType, msg.QBFTMessage.MsgType)
		require.Equal(t, specqbft.Round(2), msg.QBFTMessage.Round)
		require.Equal(t, specqbft.Round(1), msg.QBFTMessage.DataRound)
		require.Equal(t, root, msg.QBFTMessage.Root)
		require.Equal(t, fullData, msg.SignedMessage.FullData)

		prepareJustifications, err := msg.QBFTMessage.GetRoundChangeJustifications()
		require.NoError(t, err)
		require.Len(t, prepareJustifications, 3)
	})
}
