package types

import (
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

func TestState_Decoding(t *testing.T) {
	state := &State{
		Share: &spectypes.Share{
			OperatorID:      1,
			ValidatorPubKey: []byte{1, 2, 3, 4},
			Committee: []*spectypes.Operator{
				{
					OperatorID: 1,
					PubKey:     []byte{1, 2, 3, 4},
				},
			},
			DomainType: spectypes.PrimusTestnet,
		},
		ID:                []byte{1, 2, 3, 4},
		Round:             1,
		Height:            2,
		LastPreparedRound: 3,
		LastPreparedValue: []byte{1, 2, 3, 4},
		ProposalAcceptedForCurrentRound: &specqbft.SignedMessage{
			Message: &specqbft.Message{
				MsgType:    specqbft.CommitMsgType,
				Height:     1,
				Round:      2,
				Identifier: []byte{1, 2, 3, 4},
				Data:       []byte{1, 2, 3, 4},
			},
			Signature: []byte{1, 2, 3, 4},
			Signers:   []spectypes.OperatorID{1},
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
	require.EqualValues(t, spectypes.PrimusTestnet, decodedState.Share.DomainType)

	require.EqualValues(t, 3, decodedState.LastPreparedRound)
	require.EqualValues(t, []byte{1, 2, 3, 4}, decodedState.LastPreparedValue)
	require.EqualValues(t, []byte{1, 2, 3, 4}, decodedState.ID)
	require.EqualValues(t, 2, decodedState.Height)
	require.EqualValues(t, 1, decodedState.Round)

	require.EqualValues(t, []byte{1, 2, 3, 4}, decodedState.ProposalAcceptedForCurrentRound.Signature)
	require.EqualValues(t, []spectypes.OperatorID{1}, decodedState.ProposalAcceptedForCurrentRound.Signers)
	require.EqualValues(t, specqbft.CommitMsgType, decodedState.ProposalAcceptedForCurrentRound.Message.MsgType)
	require.EqualValues(t, 1, decodedState.ProposalAcceptedForCurrentRound.Message.Height)
	require.EqualValues(t, 2, decodedState.ProposalAcceptedForCurrentRound.Message.Round)
	require.EqualValues(t, []byte{1, 2, 3, 4}, decodedState.ProposalAcceptedForCurrentRound.Message.Identifier)
	require.EqualValues(t, []byte{1, 2, 3, 4}, decodedState.ProposalAcceptedForCurrentRound.Message.Data)
}
