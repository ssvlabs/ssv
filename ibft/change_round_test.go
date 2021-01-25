package ibft

import (
	"encoding/json"
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/ibft/types"
)

func changeRoundDataToBytes(input *types.ChangeRoundData) []byte {
	ret, _ := json.Marshal(input)
	return ret
}
func bytesTochangeRoundData(input []byte) *types.ChangeRoundData {
	ret := &types.ChangeRoundData{}
	json.Unmarshal(input, ret)
	return ret
}

func TestChangeRoundMessage(t *testing.T) {
	sks, nodes := generateNodes(4)
	i := &iBFTInstance{
		params: &types.InstanceParams{
			ConsensusParams: types.DefaultConsensusParams(),
			IbftCommittee:   nodes,
		},
		state: &types.State{
			Round:         1,
			PreparedRound: 0,
			PreparedValue: nil,
		},
	}

	tests := []struct {
		name                string
		msg                 *types.Message
		justificationSigIds []uint64
		expectedError       string
	}{
		{
			name: "valid",
			msg: &types.Message{
				Type:   types.RoundState_ChangeRound,
				Round:  1,
				Lambda: []byte("lambda"),
				Value:  nil,
			},
			expectedError: "",
		},
		{
			name: "valid",
			msg: &types.Message{
				Type:   types.RoundState_ChangeRound,
				Round:  2,
				Lambda: []byte("lambda"),
				Value:  nil,
			},
			expectedError: "",
		},
		{
			name: "valid",
			msg: &types.Message{
				Type:   types.RoundState_ChangeRound,
				Round:  3,
				Lambda: []byte("lambda"),
				Value:  nil,
			},
			expectedError: "",
		},
		{
			name: "valid",
			msg: &types.Message{
				Type:   types.RoundState_ChangeRound,
				Round:  3,
				Lambda: []byte("lambdas"),
				Value:  nil,
			},
			expectedError: "",
		},
		{
			name:                "valid justification",
			justificationSigIds: []uint64{0, 1, 2},
			msg: &types.Message{
				Type:   types.RoundState_ChangeRound,
				Round:  3,
				Lambda: []byte("lambdas"),
				Value: changeRoundDataToBytes(&types.ChangeRoundData{
					PreparedRound: 2,
					PreparedValue: []byte("value"),
					JustificationMsg: &types.Message{
						Type:   types.RoundState_Prepare,
						Round:  2,
						Lambda: []byte("lambdas"),
						Value:  []byte("value"),
					},
					JustificationSig: nil,
					SignedIds:        []uint64{0, 1, 2},
				}),
			},
			expectedError: "",
		},
		{
			name:                "invalid justification msg type",
			justificationSigIds: []uint64{0, 1, 2},
			msg: &types.Message{
				Type:   types.RoundState_ChangeRound,
				Round:  3,
				Lambda: []byte("lambdas"),
				Value: changeRoundDataToBytes(&types.ChangeRoundData{
					PreparedRound: 2,
					PreparedValue: []byte("value"),
					JustificationMsg: &types.Message{
						Type:   types.RoundState_Preprepare,
						Round:  2,
						Lambda: []byte("lambdas"),
						Value:  []byte("value"),
					},
					JustificationSig: nil,
					SignedIds:        []uint64{0, 1, 2},
				}),
			},
			expectedError: "change round justification msg type not Prepare",
		},
		{
			name:                "invalid justification round",
			justificationSigIds: []uint64{0, 1, 2},
			msg: &types.Message{
				Type:   types.RoundState_ChangeRound,
				Round:  3,
				Lambda: []byte("lambdas"),
				Value: changeRoundDataToBytes(&types.ChangeRoundData{
					PreparedRound: 2,
					PreparedValue: []byte("value"),
					JustificationMsg: &types.Message{
						Type:   types.RoundState_Prepare,
						Round:  3,
						Lambda: []byte("lambdas"),
						Value:  []byte("value"),
					},
					JustificationSig: nil,
					SignedIds:        []uint64{0, 1, 2},
				}),
			},
			expectedError: "change round justification round lower or equal to message round",
		},
		{
			name:                "invalid prepared and  justification round",
			justificationSigIds: []uint64{0, 1, 2},
			msg: &types.Message{
				Type:   types.RoundState_ChangeRound,
				Round:  3,
				Lambda: []byte("lambdas"),
				Value: changeRoundDataToBytes(&types.ChangeRoundData{
					PreparedRound: 2,
					PreparedValue: []byte("value"),
					JustificationMsg: &types.Message{
						Type:   types.RoundState_Prepare,
						Round:  1,
						Lambda: []byte("lambdas"),
						Value:  []byte("value"),
					},
					JustificationSig: nil,
					SignedIds:        []uint64{0, 1, 2},
				}),
			},
			expectedError: "change round prepared round not equal to justification msg round",
		},
		{
			name:                "invalid justification instance",
			justificationSigIds: []uint64{0, 1, 2},
			msg: &types.Message{
				Type:   types.RoundState_ChangeRound,
				Round:  3,
				Lambda: []byte("lambdas"),
				Value: changeRoundDataToBytes(&types.ChangeRoundData{
					PreparedRound: 2,
					PreparedValue: []byte("value"),
					JustificationMsg: &types.Message{
						Type:   types.RoundState_Prepare,
						Round:  2,
						Lambda: []byte("lambda"),
						Value:  []byte("value"),
					},
					JustificationSig: nil,
					SignedIds:        []uint64{0, 1, 2},
				}),
			},
			expectedError: "change round justification msg lambda not equal to msg lambda",
		},
		{
			name:                "valid justification",
			justificationSigIds: []uint64{0, 1, 2},
			msg: &types.Message{
				Type:   types.RoundState_ChangeRound,
				Round:  3,
				Lambda: []byte("lambdas"),
				Value: changeRoundDataToBytes(&types.ChangeRoundData{
					PreparedRound: 2,
					PreparedValue: []byte("value"),
					JustificationMsg: &types.Message{
						Type:   types.RoundState_Prepare,
						Round:  2,
						Lambda: []byte("lambdas"),
						Value:  []byte("values"),
					},
					JustificationSig: nil,
					SignedIds:        []uint64{0, 1, 2},
				}),
			},
			expectedError: "change round prepared value not equal to justification msg value",
		},
		{
			name:                "invalid justification sig",
			justificationSigIds: []uint64{0, 1},
			msg: &types.Message{
				Type:   types.RoundState_ChangeRound,
				Round:  3,
				Lambda: []byte("lambdas"),
				Value: changeRoundDataToBytes(&types.ChangeRoundData{
					PreparedRound: 2,
					PreparedValue: []byte("value"),
					JustificationMsg: &types.Message{
						Type:   types.RoundState_Prepare,
						Round:  2,
						Lambda: []byte("lambdas"),
						Value:  []byte("value"),
					},
					JustificationSig: nil,
					SignedIds:        []uint64{0, 1, 2},
				}),
			},
			expectedError: "change round justification signature doesn't verify",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// sign if needed
			if test.msg.Value != nil {
				var sig *bls.Sign
				data := bytesTochangeRoundData(test.msg.Value)
				for _, id := range test.justificationSigIds {
					s, err := data.JustificationMsg.Sign(sks[id])
					require.NoError(t, err)
					if sig == nil {
						sig = s
					} else {
						sig.Add(s)
					}
				}
				data.JustificationSig = sig.Serialize()
				test.msg.Value = changeRoundDataToBytes(data)
			}

			err := i.validateChangeRoundMsg(test.msg)
			if len(test.expectedError) > 0 {
				require.EqualError(t, err, test.expectedError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRoundChangeJustification(t *testing.T) {
	inputValue := changeRoundDataToBytes(&types.ChangeRoundData{
		PreparedRound: 1,
		PreparedValue: []byte("hello"),
	})

	i := &iBFTInstance{
		roundChangeMessages: types.NewMessagesContainer(),
		params: &types.InstanceParams{
			ConsensusParams: types.DefaultConsensusParams(),
			IbftCommittee: map[uint64]*types.Node{
				0: {IbftId: 0},
				1: {IbftId: 1},
				2: {IbftId: 2},
				3: {IbftId: 3},
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

	i.roundChangeMessages.AddMessage(types.SignedMessage{
		Message: &types.Message{
			Type:   types.RoundState_ChangeRound,
			Round:  2,
			Lambda: []byte("lambda"),
			Value:  nil,
		},
		IbftId: 1,
	})
	i.roundChangeMessages.AddMessage(types.SignedMessage{
		Message: &types.Message{
			Type:   types.RoundState_ChangeRound,
			Round:  2,
			Lambda: []byte("lambda"),
			Value:  nil,
		},
		IbftId: 2,
	})
	i.roundChangeMessages.AddMessage(types.SignedMessage{
		Message: &types.Message{
			Type:   types.RoundState_ChangeRound,
			Round:  2,
			Lambda: []byte("lambda"),
			Value:  nil,
		},
		IbftId: 3,
	})

	// test no previous prepared round with round change quorum (no justification)
	res, err = i.justifyRoundChange(2)
	require.NoError(t, err)
	require.True(t, res)

	i.roundChangeMessages = types.NewMessagesContainer()
	i.roundChangeMessages.AddMessage(types.SignedMessage{
		Message: &types.Message{
			Type:   types.RoundState_ChangeRound,
			Round:  1,
			Lambda: []byte("lambda"),
			Value:  inputValue,
		},
		IbftId: 1,
	})
	i.roundChangeMessages.AddMessage(types.SignedMessage{
		Message: &types.Message{
			Type:   types.RoundState_ChangeRound,
			Round:  2,
			Lambda: []byte("lambda"),
			Value:  inputValue,
		},
		IbftId: 2,
	})
	i.roundChangeMessages.AddMessage(types.SignedMessage{
		Message: &types.Message{
			Type:   types.RoundState_ChangeRound,
			Round:  2,
			Lambda: []byte("lambda"),
			Value:  inputValue,
		},
		IbftId: 3,
	})

	// test no previous prepared round with round change quorum (with justification)
	res, err = i.justifyRoundChange(2)
	require.Errorf(t, err, "could not justify round change, did not received quorum of prepare messages previously")
	require.False(t, res)

	i.state.PreparedRound = 1
	i.state.PreparedValue = []byte("hello")

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
			IbftCommittee: map[uint64]*types.Node{
				0: {IbftId: 0},
				1: {IbftId: 1},
				2: {IbftId: 2},
				3: {IbftId: 3},
			},
		},
	}
	i.roundChangeMessages.AddMessage(types.SignedMessage{
		Message: &types.Message{
			Type:   types.RoundState_ChangeRound,
			Round:  3,
			Lambda: []byte("lambda"),
			Value: changeRoundDataToBytes(&types.ChangeRoundData{
				PreparedRound: 1,
				PreparedValue: inputValue,
			}),
		},
		IbftId: 1,
	})
	i.roundChangeMessages.AddMessage(types.SignedMessage{
		Message: &types.Message{
			Type:   types.RoundState_ChangeRound,
			Round:  3,
			Lambda: []byte("lambda"),
			Value: changeRoundDataToBytes(&types.ChangeRoundData{
				PreparedRound: 2,
				PreparedValue: append(inputValue, []byte("highest")...),
			}),
		},
		IbftId: 2,
	})

	// test one higher than other
	res, err := i.highestPrepared(3)
	require.NoError(t, err)
	require.EqualValues(t, 2, res.PreparedRound)
	require.EqualValues(t, append(inputValue, []byte("highest")...), res.PreparedValue)

	// test 2 equals
	i.roundChangeMessages.AddMessage(types.SignedMessage{
		Message: &types.Message{
			Type:   types.RoundState_ChangeRound,
			Round:  3,
			Lambda: []byte("lambda"),
			Value: changeRoundDataToBytes(&types.ChangeRoundData{
				PreparedRound: 2,
				PreparedValue: append(inputValue, []byte("highest")...),
			}),
		},
		IbftId: 2,
	})
	res, err = i.highestPrepared(3)
	require.NoError(t, err)
	require.EqualValues(t, 2, res.PreparedRound)
	require.EqualValues(t, append(inputValue, []byte("highest")...), res.PreparedValue)
}
