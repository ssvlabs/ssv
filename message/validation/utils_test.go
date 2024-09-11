package validation

import (
	"testing"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

func TestMessageValidator_maxRound(t *testing.T) {
	tt := []struct {
		name string
		role spectypes.RunnerRole
		want specqbft.Round
		err  error
	}{
		{
			name: "Committee role",
			role: spectypes.RoleCommittee,
			want: 12,
			err:  nil,
		},
		{
			name: "Aggregator role",
			role: spectypes.RoleAggregator,
			want: 12,
			err:  nil,
		},
		{
			name: "Proposer role",
			role: spectypes.RoleProposer,
			want: 6,
			err:  nil,
		},
		{
			name: "SyncCommitteeContribution role",
			role: spectypes.RoleSyncCommitteeContribution,
			want: 6,
			err:  nil,
		},
		{
			name: "Unknown role",
			role: spectypes.RunnerRole(999),
			want: 0,
			err:  fmt.Errorf("unknown role"),
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			mv := &messageValidator{}
			got, err := mv.maxRound(tc.role)
			if tc.err != nil {
				require.EqualError(t, err, tc.err.Error())
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.want, got)
		})
	}
}

func TestRecordConsensusMessage(t *testing.T) {
	tt := []struct {
		name             string
		signedSSVMessage *spectypes.SignedSSVMessage
		msg              *specqbft.Message
		expectedCounts   MessageCounts
		expectedError    error
	}{
		{
			name:             "ProposalMessage_IncrementsProposalCount",
			signedSSVMessage: &spectypes.SignedSSVMessage{},
			msg:              &specqbft.Message{MsgType: specqbft.ProposalMsgType},
			expectedCounts:   MessageCounts{Proposal: 1},
			expectedError:    nil,
		},
		{
			name:             "PrepareMessage_IncrementsPrepareCount",
			signedSSVMessage: &spectypes.SignedSSVMessage{},
			msg:              &specqbft.Message{MsgType: specqbft.PrepareMsgType},
			expectedCounts:   MessageCounts{Prepare: 1},
			expectedError:    nil,
		},
		{
			name:             "CommitMessageWithSingleOperator_IncrementsCommitCount",
			signedSSVMessage: &spectypes.SignedSSVMessage{OperatorIDs: []spectypes.OperatorID{1}},
			msg:              &specqbft.Message{MsgType: specqbft.CommitMsgType},
			expectedCounts:   MessageCounts{Commit: 1},
			expectedError:    nil,
		},
		{
			name:             "CommitMessageWithMultipleOperators_DoesNotIncrementCommitCount",
			signedSSVMessage: &spectypes.SignedSSVMessage{OperatorIDs: []spectypes.OperatorID{1, 2}},
			msg:              &specqbft.Message{MsgType: specqbft.CommitMsgType},
			expectedCounts:   MessageCounts{},
			expectedError:    nil,
		},
		{
			name:             "RoundChangeMessage_IncrementsRoundChangeCount",
			signedSSVMessage: &spectypes.SignedSSVMessage{},
			msg:              &specqbft.Message{MsgType: specqbft.RoundChangeMsgType},
			expectedCounts:   MessageCounts{RoundChange: 1},
			expectedError:    nil,
		},
		{
			name:             "UnexpectedMessageType_ReturnsError",
			signedSSVMessage: &spectypes.SignedSSVMessage{},
			msg:              &specqbft.Message{MsgType: specqbft.MessageType(12345)},
			expectedCounts:   MessageCounts{},
			expectedError:    fmt.Errorf("unexpected signed message type"),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			counts := &MessageCounts{}
			err := counts.RecordConsensusMessage(tc.signedSSVMessage, tc.msg)

			if tc.expectedError != nil {
				require.EqualError(t, err, tc.expectedError.Error())
			} else {
				require.NoError(t, err)
			}

			require.Equal(t, tc.expectedCounts, *counts)
		})
	}
}

func TestValidateConsensusMessage(t *testing.T) {
	type input struct {
		counts           *MessageCounts
		signedSSVMessage *spectypes.SignedSSVMessage
		msg              *specqbft.Message
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
				counts:           &MessageCounts{Proposal: 2},
				signedSSVMessage: &spectypes.SignedSSVMessage{},
				msg:              &specqbft.Message{MsgType: specqbft.ProposalMsgType},
				limits:           MessageCounts{Proposal: 0},
			},
			expectedError: fmt.Errorf("message is duplicated, got proposal, having pre-consensus: 0, proposal: 2, prepare: 0, commit: 0, round change: 0, post-consensus: 0"),
		},
		{
			name: "PrepareMessage_ExceedsLimit_ReturnsError",
			input: input{
				counts:           &MessageCounts{Prepare: 2},
				signedSSVMessage: &spectypes.SignedSSVMessage{},
				msg:              &specqbft.Message{MsgType: specqbft.PrepareMsgType},
				limits:           MessageCounts{Prepare: 0},
			},
			expectedError: fmt.Errorf("message is duplicated, got prepare, having pre-consensus: 0, proposal: 0, prepare: 2, commit: 0, round change: 0, post-consensus: 0"),
		},
		{
			name: "CommitMessageWithSingleOperator_ExceedsLimit_ReturnsError",
			input: input{
				counts:           &MessageCounts{Commit: 2},
				signedSSVMessage: &spectypes.SignedSSVMessage{OperatorIDs: []spectypes.OperatorID{1}},
				msg:              &specqbft.Message{MsgType: specqbft.CommitMsgType},
				limits:           MessageCounts{Commit: 0},
			},
			expectedError: fmt.Errorf("message is duplicated, got commit, having pre-consensus: 0, proposal: 0, prepare: 0, commit: 2, round change: 0, post-consensus: 0"),
		},
		{
			name: "RoundChangeMessage_ExceedsLimit_ReturnsError",
			input: input{
				counts:           &MessageCounts{RoundChange: 2},
				signedSSVMessage: &spectypes.SignedSSVMessage{},
				msg:              &specqbft.Message{MsgType: specqbft.RoundChangeMsgType},
				limits:           MessageCounts{RoundChange: 0},
			},
			expectedError: fmt.Errorf("message is duplicated, got round change, having pre-consensus: 0, proposal: 0, prepare: 0, commit: 0, round change: 2, post-consensus: 0"),
		},
		{
			name: "UnexpectedMessageType_ReturnsError",
			input: input{
				counts:           &MessageCounts{},
				signedSSVMessage: &spectypes.SignedSSVMessage{},
				msg:              &specqbft.Message{MsgType: specqbft.MessageType(12345)},
				limits:           MessageCounts{},
			},
			expectedError: fmt.Errorf("unexpected signed message type"),
		},
		{
			name: "ValidProposalMessage_HappyFlow",
			input: input{
				counts:           &MessageCounts{Proposal: 2},
				signedSSVMessage: &spectypes.SignedSSVMessage{},
				msg:              &specqbft.Message{MsgType: specqbft.ProposalMsgType},
				limits:           MessageCounts{Proposal: 100},
			},
			expectedError: nil,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.input.counts.ValidateConsensusMessage(tc.input.signedSSVMessage, tc.input.msg, tc.input.limits)

			if tc.expectedError != nil {
				require.EqualError(t, err, tc.expectedError.Error())
			} else {
				require.NoError(t, err)
			}

		})
	}
}
