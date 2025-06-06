package validation

import (
	"fmt"
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
			role: 999,
			want: 0,
			err:  fmt.Errorf("unknown role"),
		},
	}

	for _, tc := range tt {
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
		expectedSeen     SeenMsgTypes
		expectedError    error
	}{
		{
			name:             "ProposalMessage_IncrementsProposalCount",
			signedSSVMessage: &spectypes.SignedSSVMessage{},
			msg:              &specqbft.Message{MsgType: specqbft.ProposalMsgType},
			expectedSeen:     SeenMsgTypes{v: 0b10},
			expectedError:    nil,
		},
		{
			name:             "PrepareMessage_IncrementsPrepareCount",
			signedSSVMessage: &spectypes.SignedSSVMessage{},
			msg:              &specqbft.Message{MsgType: specqbft.PrepareMsgType},
			expectedSeen:     SeenMsgTypes{v: 0b100},
			expectedError:    nil,
		},
		{
			name:             "CommitMessageWithSingleOperator_IncrementsCommitCount",
			signedSSVMessage: &spectypes.SignedSSVMessage{OperatorIDs: []spectypes.OperatorID{1}},
			msg:              &specqbft.Message{MsgType: specqbft.CommitMsgType},
			expectedSeen:     SeenMsgTypes{v: 0b1000},
			expectedError:    nil,
		},
		{
			name:             "CommitMessageWithMultipleOperators_DoesNotIncrementCommitCount",
			signedSSVMessage: &spectypes.SignedSSVMessage{OperatorIDs: []spectypes.OperatorID{1, 2}},
			msg:              &specqbft.Message{MsgType: specqbft.CommitMsgType},
			expectedSeen:     SeenMsgTypes{},
			expectedError:    nil,
		},
		{
			name:             "RoundChangeMessage_IncrementsRoundChangeCount",
			signedSSVMessage: &spectypes.SignedSSVMessage{},
			msg:              &specqbft.Message{MsgType: specqbft.RoundChangeMsgType},
			expectedSeen:     SeenMsgTypes{v: 0b10000},
			expectedError:    nil,
		},
		{
			name:             "UnexpectedMessageType_ReturnsError",
			signedSSVMessage: &spectypes.SignedSSVMessage{},
			msg:              &specqbft.Message{MsgType: specqbft.MessageType(12345)},
			expectedSeen:     SeenMsgTypes{},
			expectedError:    fmt.Errorf("unexpected signed message type"),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			seen := &SeenMsgTypes{}
			err := seen.RecordConsensusMessage(tc.signedSSVMessage, tc.msg)

			if tc.expectedError != nil {
				require.EqualError(t, err, tc.expectedError.Error())
			} else {
				require.NoError(t, err)
			}

			require.Equal(t, tc.expectedSeen, *seen)
		})
	}
}

func TestValidateConsensusMessage(t *testing.T) {
	type input struct {
		seenMsgTypes     *SeenMsgTypes
		signedSSVMessage *spectypes.SignedSSVMessage
		msg              *specqbft.Message
	}

	tt := []struct {
		name          string
		input         input
		expectedError error
	}{
		{
			name: "ProposalMessage_ExceedsLimit_ReturnsError",
			input: input{
				seenMsgTypes:     &SeenMsgTypes{v: 0b10},
				signedSSVMessage: &spectypes.SignedSSVMessage{},
				msg:              &specqbft.Message{MsgType: specqbft.ProposalMsgType},
			},
			expectedError: fmt.Errorf("message is duplicated, got proposal, having proposal"),
		},
		{
			name: "PrepareMessage_ExceedsLimit_ReturnsError",
			input: input{
				seenMsgTypes:     &SeenMsgTypes{v: 0b100},
				signedSSVMessage: &spectypes.SignedSSVMessage{},
				msg:              &specqbft.Message{MsgType: specqbft.PrepareMsgType},
			},
			expectedError: fmt.Errorf("message is duplicated, got prepare, having prepare"),
		},
		{
			name: "CommitMessageWithSingleOperator_ExceedsLimit_ReturnsError",
			input: input{
				seenMsgTypes:     &SeenMsgTypes{v: 0b1000},
				signedSSVMessage: &spectypes.SignedSSVMessage{OperatorIDs: []spectypes.OperatorID{1}},
				msg:              &specqbft.Message{MsgType: specqbft.CommitMsgType},
			},
			expectedError: fmt.Errorf("message is duplicated, got commit, having commit"),
		},
		{
			name: "RoundChangeMessage_ExceedsLimit_ReturnsError",
			input: input{
				seenMsgTypes:     &SeenMsgTypes{v: 0b10000},
				signedSSVMessage: &spectypes.SignedSSVMessage{},
				msg:              &specqbft.Message{MsgType: specqbft.RoundChangeMsgType},
			},
			expectedError: fmt.Errorf("message is duplicated, got round change, having round change"),
		},
		{
			name: "UnexpectedMessageType_ReturnsError",
			input: input{
				seenMsgTypes:     &SeenMsgTypes{},
				signedSSVMessage: &spectypes.SignedSSVMessage{},
				msg:              &specqbft.Message{MsgType: specqbft.MessageType(12345)},
			},
			expectedError: fmt.Errorf("unexpected signed message type"),
		},
		{
			name: "ValidProposalMessage_HappyFlow",
			input: input{
				seenMsgTypes:     &SeenMsgTypes{v: 0b0},
				signedSSVMessage: &spectypes.SignedSSVMessage{},
				msg:              &specqbft.Message{MsgType: specqbft.ProposalMsgType},
			},
			expectedError: nil,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.input.seenMsgTypes.ValidateConsensusMessage(tc.input.signedSSVMessage, tc.input.msg)

			if tc.expectedError != nil {
				require.EqualError(t, err, tc.expectedError.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}
