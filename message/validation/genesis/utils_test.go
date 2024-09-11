package validation

import (
	"testing"

	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

func TestRecordConsensusMessage(t *testing.T) {
	tt := []struct {
		name           string
		msg            *specqbft.SignedMessage
		initialCounts  MessageCounts
		expectedCounts MessageCounts
		expectedError  error
	}{
		{
			name: "ProposalMessage_IncrementsProposalCount",
			msg: &specqbft.SignedMessage{
				Message: specqbft.Message{MsgType: specqbft.ProposalMsgType},
			},
			initialCounts:  MessageCounts{},
			expectedCounts: MessageCounts{Proposal: 1},
			expectedError:  nil,
		},
		{
			name: "PrepareMessage_IncrementsPrepareCount",
			msg: &specqbft.SignedMessage{
				Message: specqbft.Message{MsgType: specqbft.PrepareMsgType},
			},
			initialCounts:  MessageCounts{},
			expectedCounts: MessageCounts{Prepare: 1},
			expectedError:  nil,
		},
		{
			name: "CommitMessageWithSingleOperator_IncrementsCommitCount",
			msg: &specqbft.SignedMessage{
				Message: specqbft.Message{MsgType: specqbft.CommitMsgType},
				Signers: []spectypes.OperatorID{1},
			},
			initialCounts:  MessageCounts{},
			expectedCounts: MessageCounts{Commit: 1},
			expectedError:  nil,
		},
		{
			name: "CommitMessageWithMultipleOperators_IncrementsDecidedCount",
			msg: &specqbft.SignedMessage{
				Message: specqbft.Message{MsgType: specqbft.CommitMsgType},
				Signers: []spectypes.OperatorID{1, 2},
			},
			initialCounts:  MessageCounts{},
			expectedCounts: MessageCounts{Decided: 1},
			expectedError:  nil,
		},
		{
			name: "RoundChangeMessage_IncrementsRoundChangeCount",
			msg: &specqbft.SignedMessage{
				Message: specqbft.Message{MsgType: specqbft.RoundChangeMsgType},
			},
			initialCounts:  MessageCounts{},
			expectedCounts: MessageCounts{RoundChange: 1},
			expectedError:  nil,
		},
		{
			name: "UnexpectedMessageType_ReturnsError",
			msg: &specqbft.SignedMessage{
				Message: specqbft.Message{MsgType: specqbft.MessageType(12345)},
			},
			initialCounts:  MessageCounts{},
			expectedCounts: MessageCounts{},
			expectedError:  errors.New("unexpected signed message type"),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			counts := tc.initialCounts
			err := counts.RecordConsensusMessage(tc.msg)

			if tc.expectedError != nil {
				require.EqualError(t, err, tc.expectedError.Error())
			} else {
				require.NoError(t, err)
			}

			require.Equal(t, tc.expectedCounts, counts)
		})
	}
}

func TestValidateConsensusMessage(t *testing.T) {
	type input struct {
		signedSSVMessage *specqbft.SignedMessage
		counts           *MessageCounts
		limits           MessageCounts
	}

	tt := []struct {
		name          string
		input         input
		expectedError error
	}{
		{
			name: "ProposalMessage_ExceedsLimit_ReturnsError",
			input: input{
				counts: &MessageCounts{Proposal: 2},
				signedSSVMessage: &specqbft.SignedMessage{
					Message: specqbft.Message{MsgType: specqbft.ProposalMsgType},
				},
				limits: MessageCounts{Proposal: 1},
			},
			expectedError: errors.New("too many messages of same type per round, got proposal, having pre-consensus: 0, proposal: 2, prepare: 0, commit: 0, decided: 0, round change: 0, post-consensus: 0"),
		},
		{
			name: "PrepareMessage_ExceedsLimit_ReturnsError",
			input: input{
				counts: &MessageCounts{Prepare: 2},
				signedSSVMessage: &specqbft.SignedMessage{
					Message: specqbft.Message{MsgType: specqbft.PrepareMsgType},
				},
				limits: MessageCounts{Prepare: 1},
			},
			expectedError: errors.New("too many messages of same type per round, got prepare, having pre-consensus: 0, proposal: 0, prepare: 2, commit: 0, decided: 0, round change: 0, post-consensus: 0"),
		},
		{
			name: "CommitMessageWithSingleOperator_ExceedsLimit_ReturnsError",
			input: input{
				counts: &MessageCounts{Commit: 2},
				signedSSVMessage: &specqbft.SignedMessage{
					Message: specqbft.Message{MsgType: specqbft.CommitMsgType},
					Signers: []spectypes.OperatorID{1},
				},
				limits: MessageCounts{Commit: 0},
			},
			expectedError: errors.New("too many messages of same type per round, got commit, having pre-consensus: 0, proposal: 0, prepare: 0, commit: 2, decided: 0, round change: 0, post-consensus: 0"),
		},
		{
			name: "CommitMessageWithManyOperators_ExceedsLimit_ReturnsError",
			input: input{
				counts: &MessageCounts{Commit: 2},
				signedSSVMessage: &specqbft.SignedMessage{
					Message: specqbft.Message{MsgType: specqbft.CommitMsgType},
					Signers: []spectypes.OperatorID{1, 2, 3},
				},
				limits: MessageCounts{Commit: 1},
			},
			expectedError: errors.New("too many messages of same type per round, got decided, having pre-consensus: 0, proposal: 0, prepare: 0, commit: 2, decided: 0, round change: 0, post-consensus: 0"),
		},
		{
			name: "RoundChangeMessage_ExceedsLimit_ReturnsError",
			input: input{
				counts: &MessageCounts{RoundChange: 2},
				signedSSVMessage: &specqbft.SignedMessage{
					Message: specqbft.Message{MsgType: specqbft.RoundChangeMsgType},
				},
				limits: MessageCounts{RoundChange: 1},
			},
			expectedError: errors.New("too many messages of same type per round, got round change, having pre-consensus: 0, proposal: 0, prepare: 0, commit: 0, decided: 0, round change: 2, post-consensus: 0"),
		},
		{
			name: "UnexpectedMessageType_ReturnsError",
			input: input{
				counts:           &MessageCounts{},
				signedSSVMessage: &specqbft.SignedMessage{Message: specqbft.Message{MsgType: specqbft.MessageType(12345)}},
				limits:           MessageCounts{},
			},
			expectedError: errors.New("unexpected signed message type"),
		},
		{
			name: "ValidProposalMessage_HappyFlow",
			input: input{
				counts: &MessageCounts{Proposal: 2},
				signedSSVMessage: &specqbft.SignedMessage{
					Message: specqbft.Message{MsgType: specqbft.ProposalMsgType},
				},
				limits: MessageCounts{Proposal: 100},
			},
			expectedError: nil,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {

			err := tc.input.counts.ValidateConsensusMessage(tc.input.signedSSVMessage, tc.input.limits)

			if tc.expectedError != nil {
				require.EqualError(t, err, tc.expectedError.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}
