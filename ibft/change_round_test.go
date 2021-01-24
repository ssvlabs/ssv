package ibft

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/ibft/types"
)

func inputDataToBytes(input *roundChangeData) []byte {
	ret, _ := json.Marshal(input)
	return ret
}

func TestRoundChangeJustification(t *testing.T) {
	inputValue := []byte("input value")

	i := &iBFTInstance{
		roundChangeMessages: types.NewMessagesContainer(),
		params: &types.InstanceParams{
			ConsensusParams: types.DefaultConsensusParams(),
			IbftCommittee: []*types.Node{
				{IbftId: 0},
				{IbftId: 1},
				{IbftId: 2},
				{IbftId: 3},
			},
		},
		state: &types.State{
			Round:         1,
			PreparedRound: 0,
			PreparedValue: nil,
		},
	}

	// test no previous prepared round and no round change quorum
	res, err := i.justifyRoundChange(2)
	require.EqualError(t, err, "could not justify round change, did not find highest prepared")
	require.False(t, res)

	i.roundChangeMessages.AddMessage(types.Message{
		Type:       types.RoundState_RoundChange,
		IbftId:     1,
		Round:      2,
		Lambda:     []byte("lambda"),
		InputValue: nil,
	})
	i.roundChangeMessages.AddMessage(types.Message{
		Type:       types.RoundState_RoundChange,
		IbftId:     2,
		Round:      2,
		Lambda:     []byte("lambda"),
		InputValue: nil,
	})
	i.roundChangeMessages.AddMessage(types.Message{
		Type:       types.RoundState_RoundChange,
		IbftId:     3,
		Round:      2,
		Lambda:     []byte("lambda"),
		InputValue: nil,
	})

	// test no previous prepared round with round change quorum (no justification)
	res, err = i.justifyRoundChange(2)
	require.NoError(t, err)
	require.True(t, res)

	i.roundChangeMessages = types.NewMessagesContainer()
	i.roundChangeMessages.AddMessage(types.Message{
		Type:   types.RoundState_RoundChange,
		IbftId: 1,
		Round:  2,
		Lambda: []byte("lambda"),
		InputValue: inputDataToBytes(&roundChangeData{
			PreparedRound: 1,
			PreparedValue: inputValue,
		}),
	})
	i.roundChangeMessages.AddMessage(types.Message{
		Type:   types.RoundState_RoundChange,
		IbftId: 2,
		Round:  2,
		Lambda: []byte("lambda"),
		InputValue: inputDataToBytes(&roundChangeData{
			PreparedRound: 1,
			PreparedValue: inputValue,
		}),
	})
	i.roundChangeMessages.AddMessage(types.Message{
		Type:   types.RoundState_RoundChange,
		IbftId: 3,
		Round:  2,
		Lambda: []byte("lambda"),
		InputValue: inputDataToBytes(&roundChangeData{
			PreparedRound: 1,
			PreparedValue: inputValue,
		}),
	})

	// test no previous prepared round with round change quorum (with justification)
	res, err = i.justifyRoundChange(2)
	require.Errorf(t, err, "could not justify round change, did not received quorum of prepare messages previously")
	require.False(t, res)

	i.state.PreparedRound = 1
	i.state.PreparedValue = inputValue

	// test previously prepared round with round change quorum (with justification)
	res, err = i.justifyRoundChange(2)
	require.NoError(t, err)
	require.True(t, res)
}

func TestHighestPrepared(t *testing.T) {
	inputValue := []byte("input value")

	i := &iBFTInstance{
		roundChangeMessages: types.NewMessagesContainer(),
		params: &types.InstanceParams{
			ConsensusParams: types.DefaultConsensusParams(),
			IbftCommittee: []*types.Node{
				{IbftId: 0},
				{IbftId: 1},
				{IbftId: 2},
				{IbftId: 3},
			},
		},
	}
	i.roundChangeMessages.AddMessage(types.Message{
		Type:   types.RoundState_RoundChange,
		Round:  3,
		Lambda: []byte("lambda"),
		InputValue: inputDataToBytes(&roundChangeData{
			PreparedRound: 1,
			PreparedValue: inputValue,
		}),
	})
	i.roundChangeMessages.AddMessage(types.Message{
		Type:   types.RoundState_RoundChange,
		Round:  3,
		Lambda: []byte("lambda"),
		InputValue: inputDataToBytes(&roundChangeData{
			PreparedRound: 2,
			PreparedValue: append(inputValue, []byte("highest")...),
		}),
	})

	// test one higher than other
	res, err := i.highestPrepared(3)
	require.NoError(t, err)
	require.EqualValues(t, 2, res.PreparedRound)
	require.EqualValues(t, append(inputValue, []byte("highest")...), res.PreparedValue)

	// test 2 equals
	i.roundChangeMessages.AddMessage(types.Message{
		Type:   types.RoundState_RoundChange,
		Round:  3,
		Lambda: []byte("lambda"),
		InputValue: inputDataToBytes(&roundChangeData{
			PreparedRound: 2,
			PreparedValue: append(inputValue, []byte("highest")...),
		}),
	})
	res, err = i.highestPrepared(3)
	require.NoError(t, err)
	require.EqualValues(t, 2, res.PreparedRound)
	require.EqualValues(t, append(inputValue, []byte("highest")...), res.PreparedValue)
}
