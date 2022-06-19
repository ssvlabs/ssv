package qbft

import (
	"github.com/bloxapp/ssv/spec/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestState_Decoding(t *testing.T) {
	state := &State{
		Share: &types.Share{
			OperatorID:      1,
			ValidatorPubKey: []byte{1, 2, 3, 4},
			Committee: []*types.Operator{
				{
					OperatorID: 1,
					PubKey:     []byte{1, 2, 3, 4},
				},
			},
			DomainType: types.PrimusTestnet,
		},
		ID:                []byte{1, 2, 3, 4},
		Round:             1,
		Height:            2,
		LastPreparedRound: 3,
		LastPreparedValue: []byte{1, 2, 3, 4},
		ProposalAcceptedForCurrentRound: &SignedMessage{
			Message: &Message{
				MsgType:    CommitMsgType,
				Height:     1,
				Round:      2,
				Identifier: []byte{1, 2, 3, 4},
				Data:       []byte{1, 2, 3, 4},
			},
			Signature: []byte{1, 2, 3, 4},
			Signers:   []types.OperatorID{1},
		},
	}

	byts, err := state.Encode()
	require.NoError(t, err)

	decodedState := &State{}
	require.NoError(t, decodedState.Decode(byts))

	require.EqualValues(t, 1, decodedState.Share.OperatorID)
	require.EqualValues(t, []byte{1, 2, 3, 4}, decodedState.Share.ValidatorPubKey)
	require.EqualValues(t, []byte{1, 2, 3, 4}, decodedState.Share.Committee[0].PubKey)
	require.EqualValues(t, 1, decodedState.Share.Committee[0].OperatorID)
	require.EqualValues(t, types.PrimusTestnet, decodedState.Share.DomainType)

	require.EqualValues(t, 3, decodedState.LastPreparedRound)
	require.EqualValues(t, []byte{1, 2, 3, 4}, decodedState.LastPreparedValue)
	require.EqualValues(t, []byte{1, 2, 3, 4}, decodedState.ID)
	require.EqualValues(t, 2, decodedState.Height)
	require.EqualValues(t, 1, decodedState.Round)

	require.EqualValues(t, []byte{1, 2, 3, 4}, decodedState.ProposalAcceptedForCurrentRound.Signature)
	require.EqualValues(t, []types.OperatorID{1}, decodedState.ProposalAcceptedForCurrentRound.Signers)
	require.EqualValues(t, CommitMsgType, decodedState.ProposalAcceptedForCurrentRound.Message.MsgType)
	require.EqualValues(t, 1, decodedState.ProposalAcceptedForCurrentRound.Message.Height)
	require.EqualValues(t, 2, decodedState.ProposalAcceptedForCurrentRound.Message.Round)
	require.EqualValues(t, []byte{1, 2, 3, 4}, decodedState.ProposalAcceptedForCurrentRound.Message.Identifier)
	require.EqualValues(t, []byte{1, 2, 3, 4}, decodedState.ProposalAcceptedForCurrentRound.Message.Data)
}
